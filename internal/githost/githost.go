// Package githost talks to the service hosting the content repository:
// opening issues for reports, reading their state and verifying webhooks.
package githost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Host is a git hosting service.
type Host interface {
	// OpenIssue opens an issue.
	OpenIssue(ctx context.Context, title, body string) (PR, error)
	// EditIssue replaces the body of an issue.
	EditIssue(ctx context.Context, number int, body string) error
	// Attach uploads a file to an issue and returns its URL. Hosts without
	// an API for that return ErrUnsupported.
	Attach(ctx context.Context, number int, name string, data []byte) (string, error)
	// EnsureLabel creates the label if the repository does not have it.
	EnsureLabel(ctx context.Context, name, color, description string) error
	// State returns the state of a pull request or an issue.
	State(ctx context.Context, number int, isPR bool) (State, error)
	// LastReviewComment returns the most recent non-empty review or
	// conversation comment on a pull request or issue, or "".
	LastReviewComment(ctx context.Context, number int) (string, error)
	// ParseWebhook verifies the signature of a webhook delivery and decodes
	// the events the bot cares about. Other events return Event{Kind: Ignored}.
	ParseWebhook(header http.Header, body []byte) (Event, error)
	// FileURL links to a file at a commit or branch; RawURL to its content.
	FileURL(ref, path string) string
	RawURL(ref, path string) string
}

// ErrUnsupported is returned for operations a host cannot do.
var ErrUnsupported = errors.New("not supported by this host")

// PR is an opened pull request or issue.
type PR struct {
	Number int
	URL    string
}

// State is the state of a pull request or issue.
type State struct {
	Closed   bool
	Merged   bool
	Labels   []string
	ClosedAt time.Time
}

// HasLabel reports whether the state carries the label.
func (s State) HasLabel(name string) bool {
	for _, l := range s.Labels {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

// EventKind classifies webhook deliveries.
type EventKind int

const (
	Ignored EventKind = iota
	PRMerged
	PRClosed // closed without merging
	Push
	// IssueChanged: an issue was closed, reopened or relabeled. The bot
	// reads its state from the API rather than trusting the delivery,
	// which could be replayed or arrive out of order.
	IssueChanged
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

// Config selects and configures a host.
type Config struct {
	Kind   string // "github" or "forgejo"
	APIURL string
	// WebURL is the repository's web address, e.g. https://github.com/o/r.
	WebURL string
	Repo   string // owner/name
	Token  string
	Secret string // webhook secret
}

// New returns the host named by cfg.Kind.
func New(cfg Config) (Host, error) {
	kind, apiURL := cfg.Kind, cfg.APIURL
	owner, name, ok := strings.Cut(cfg.Repo, "/")
	if !ok {
		return nil, fmt.Errorf("repository must be owner/name")
	}
	c := &client{http: &http.Client{Timeout: 30 * time.Second}, owner: owner, repo: name, token: cfg.Token,
		secret: []byte(cfg.Secret), web: strings.TrimSuffix(strings.TrimRight(cfg.WebURL, "/"), ".git")}
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

func multipartFile(field, name string, data []byte) (*multipartBody, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, name)
	if err != nil {
		return nil, err
	}
	part.Write(data)
	if err := w.Close(); err != nil {
		return nil, err
	}
	return &multipartBody{data: buf.Bytes(), contentType: w.FormDataContentType()}, nil
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
	web         string
}

func (c *client) repoPath(format string, args ...any) string {
	return fmt.Sprintf("%s/repos/%s/%s", c.base, c.owner, c.repo) + fmt.Sprintf(format, args...)
}

func (c *client) do(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	contentType := ""
	switch v := in.(type) {
	case nil:
	case *multipartBody:
		body, contentType = bytes.NewReader(v.data), v.contentType
	default:
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, contentType = bytes.NewReader(data), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authScheme+" "+c.token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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

type multipartBody struct {
	data        []byte
	contentType string
}

func (c *client) openIssue(ctx context.Context, title, body string) (PR, error) {
	var out struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	in := map[string]string{"title": title, "body": body}
	if err := c.do(ctx, http.MethodPost, c.repoPath("/issues"), in, &out); err != nil {
		return PR{}, err
	}
	return PR{Number: out.Number, URL: out.HTMLURL}, nil
}

func (c *client) editIssue(ctx context.Context, number int, body string) error {
	return c.do(ctx, http.MethodPatch, c.repoPath("/issues/%d", number), map[string]string{"body": body}, nil)
}

// issueJSON is the shape of issues and pull requests in both APIs.
type issueJSON struct {
	Number   int        `json:"number"`
	State    string     `json:"state"`
	Merged   bool       `json:"merged"`
	ClosedAt *time.Time `json:"closed_at"`
	Labels   []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *json.RawMessage `json:"pull_request"`
}

func (i issueJSON) state() State {
	s := State{Closed: i.State == "closed", Merged: i.Merged}
	if i.ClosedAt != nil {
		s.ClosedAt = *i.ClosedAt
	}
	for _, l := range i.Labels {
		s.Labels = append(s.Labels, l.Name)
	}
	return s
}

func (c *client) state(ctx context.Context, number int, isPR bool) (State, error) {
	var out issueJSON
	kind := "issues"
	if isPR {
		kind = "pulls"
	}
	if err := c.do(ctx, http.MethodGet, c.repoPath("/%s/%d", kind, number), nil, &out); err != nil {
		return State{}, err
	}
	return out.state(), nil
}

// ensureLabel lists the labels page by page and creates the label if it is
// missing. pageParam is the page size parameter of the host's API.
func (c *client) ensureLabel(ctx context.Context, name, color, description, pageParam string) error {
	for page := 1; page <= 20; page++ {
		var labels []struct {
			Name string `json:"name"`
		}
		if err := c.do(ctx, http.MethodGet, c.repoPath("/labels?%s=50&page=%d", pageParam, page), nil, &labels); err != nil {
			return err
		}
		for _, l := range labels {
			if strings.EqualFold(l.Name, name) {
				return nil
			}
		}
		if len(labels) < 50 {
			break
		}
	}
	in := map[string]string{"name": name, "color": color, "description": description}
	return c.do(ctx, http.MethodPost, c.repoPath("/labels"), in, nil)
}

func (c *client) fileURL(format, ref, path string) string {
	return c.web + fmt.Sprintf(format, ref) + "/" + escapePath(path)
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// webhookPayload covers the pull_request, issues and push events of both
// hosts.
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
	Issue *issueJSON `json:"issue"`
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
	case "issues":
		if err := json.Unmarshal(body, &p); err != nil {
			return Event{}, err
		}
		if p.Issue == nil || p.Issue.PullRequest != nil && string(*p.Issue.PullRequest) != "null" {
			return Event{Kind: Ignored}, nil
		}
		switch p.Action {
		case "closed", "reopened", "labeled", "unlabeled", "label_updated", "label_cleared":
			return Event{Kind: IssueChanged, PRNumber: p.Issue.Number}, nil
		}
	}
	return Event{Kind: Ignored}, nil
}
