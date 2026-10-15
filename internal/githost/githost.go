// Package githost talks to the service hosting the content repository:
// opening pull requests, reading reviews and verifying webhooks.
package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Host is a git hosting service.
type Host interface {
	// OpenPR opens a pull request from head into base.
	OpenPR(ctx context.Context, pr NewPR) (PR, error)
	// LastReviewComment returns the most recent non-empty review or
	// conversation comment on a pull request, or "".
	LastReviewComment(ctx context.Context, number int) (string, error)
	// ParseWebhook verifies the signature of a webhook delivery and decodes
	// the events the bot cares about. Other events return Event{Kind: Ignored}.
	ParseWebhook(header http.Header, body []byte) (Event, error)
}

// NewPR describes a pull request to open.
type NewPR struct {
	Title, Body, Head, Base string
}

// PR is an opened pull request.
type PR struct {
	Number int
	URL    string
}

// EventKind classifies webhook deliveries.
type EventKind int

const (
	Ignored EventKind = iota
	PRMerged
	PRClosed // closed without merging
	Push
)

// Event is a decoded webhook delivery.
type Event struct {
	Kind     EventKind
	PRNumber int
	// HeadBranch is the source branch of a pull request.
	HeadBranch string
	// Ref is the pushed ref, e.g. refs/heads/main.
	Ref string
}

// ErrSignature is returned for deliveries with a missing or wrong signature.
var ErrSignature = fmt.Errorf("invalid webhook signature")

// New returns the host named by kind ("github" or "forgejo").
func New(kind, apiURL, repo, token, secret string) (Host, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("repository must be owner/name")
	}
	c := &client{http: &http.Client{Timeout: 30 * time.Second}, owner: owner, repo: name, token: token, secret: []byte(secret)}
	switch kind {
	case "github":
		c.base = strings.TrimRight(orDefault(apiURL, "https://api.github.com"), "/")
		c.authScheme = "Bearer"
		c.extraHeader = map[string]string{"X-GitHub-Api-Version": "2022-11-28", "Accept": "application/vnd.github+json"}
		return &GitHub{c}, nil
	case "forgejo":
		if apiURL == "" {
			return nil, fmt.Errorf("forgejo needs an API URL")
		}
		c.base = strings.TrimRight(apiURL, "/") + "/api/v1"
		c.authScheme = "token"
		return &Forgejo{c}, nil
	}
	return nil, fmt.Errorf("unknown git host %q", kind)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

type client struct {
	http        *http.Client
	base        string
	owner, repo string
	token       string
	secret      []byte
	authScheme  string
	extraHeader map[string]string
}

func (c *client) repoPath(format string, args ...any) string {
	return fmt.Sprintf("%s/repos/%s/%s", c.base, c.owner, c.repo) + fmt.Sprintf(format, args...)
}

func (c *client) do(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authScheme+" "+c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.extraHeader {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s: %s", method, url, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// comment is the common shape of reviews and issue comments.
type comment struct {
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
	CreatedAt   time.Time `json:"created_at"`
}

func (c comment) time() time.Time {
	if !c.SubmittedAt.IsZero() {
		return c.SubmittedAt
	}
	return c.CreatedAt
}

// lastComment reads reviews and conversation comments and returns the
// newest non-empty body.
func (c *client) lastComment(ctx context.Context, number int) (string, error) {
	// Either list may be unreadable with a narrow token; use what is there.
	var reviews, comments []comment
	errReviews := c.do(ctx, http.MethodGet, c.repoPath("/pulls/%d/reviews", number), nil, &reviews)
	errComments := c.do(ctx, http.MethodGet, c.repoPath("/issues/%d/comments", number), nil, &comments)
	if errReviews != nil && errComments != nil {
		return "", errReviews
	}
	var best comment
	for _, x := range append(reviews, comments...) {
		if strings.TrimSpace(x.Body) != "" && !x.time().Before(best.time()) {
			best = x
		}
	}
	return strings.TrimSpace(best.Body), nil
}

func (c *client) openPR(ctx context.Context, pr NewPR) (PR, error) {
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	in := map[string]string{"title": pr.Title, "body": pr.Body, "head": pr.Head, "base": pr.Base}
	if err := c.do(ctx, http.MethodPost, c.repoPath("/pulls"), in, &out); err != nil {
		return PR{}, err
	}
	return PR{Number: out.Number, URL: out.HTMLURL}, nil
}

// webhookPayload covers the pull_request and push events of both hosts.
type webhookPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	Ref         string `json:"ref"`
	PullRequest *struct {
		Number int  `json:"number"`
		Merged bool `json:"merged"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
}

func decodeEvent(event string, body []byte) (Event, error) {
	var p webhookPayload
	switch event {
	case "pull_request":
		if err := json.Unmarshal(body, &p); err != nil {
			return Event{}, err
		}
		if p.PullRequest == nil || p.Action != "closed" {
			return Event{Kind: Ignored}, nil
		}
		ev := Event{Kind: PRClosed, PRNumber: p.PullRequest.Number, HeadBranch: p.PullRequest.Head.Ref}
		if ev.PRNumber == 0 {
			ev.PRNumber = p.Number
		}
		if p.PullRequest.Merged {
			ev.Kind = PRMerged
		}
		return ev, nil
	case "push":
		if err := json.Unmarshal(body, &p); err != nil {
			return Event{}, err
		}
		return Event{Kind: Push, Ref: p.Ref}, nil
	}
	return Event{Kind: Ignored}, nil
}
