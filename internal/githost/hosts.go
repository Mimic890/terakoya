package githost

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// GitHub uses the REST API with a fine-grained personal access token that
// has read and write access to contents and issues, and read access to
// pull requests.
type GitHub struct{ c *client }

func (g *GitHub) OpenIssue(ctx context.Context, title, body string) (PR, error) {
	return g.c.openIssue(ctx, title, body)
}

func (g *GitHub) EditIssue(ctx context.Context, number int, body string) error {
	return g.c.editIssue(ctx, number, body)
}

// Attach is not available: GitHub has no API for issue attachments.
func (g *GitHub) Attach(context.Context, int, string, []byte) (string, error) {
	return "", ErrUnsupported
}

func (g *GitHub) EnsureLabel(ctx context.Context, name, color, description string) error {
	return g.c.ensureLabel(ctx, name, strings.TrimPrefix(color, "#"), description, "per_page")
}

func (g *GitHub) State(ctx context.Context, number int, isPR bool) (State, error) {
	return g.c.state(ctx, number, isPR)
}

func (g *GitHub) FileURL(ref, path string) string { return g.c.fileURL("/blob/%s", ref, path) }
func (g *GitHub) RawURL(ref, path string) string  { return g.c.fileURL("/raw/%s", ref, path) }

func (g *GitHub) LastReviewComment(ctx context.Context, number int) (string, error) {
	return g.c.lastComment(ctx, number)
}

// ParseWebhook checks X-Hub-Signature-256 ("sha256=" + hex HMAC-SHA256).
func (g *GitHub) ParseWebhook(h http.Header, body []byte) (Event, error) {
	sig, ok := strings.CutPrefix(h.Get("X-Hub-Signature-256"), "sha256=")
	if !ok || !validMAC(g.c.secret, body, sig) {
		return Event{}, ErrSignature
	}
	return decodeEvent(h.Get("X-GitHub-Event"), body)
}

// Forgejo uses the Gitea-compatible API with an access token that has
// repository read and write scope.
type Forgejo struct{ c *client }

func (f *Forgejo) OpenIssue(ctx context.Context, title, body string) (PR, error) {
	return f.c.openIssue(ctx, title, body)
}

func (f *Forgejo) EditIssue(ctx context.Context, number int, body string) error {
	return f.c.editIssue(ctx, number, body)
}

// Attach uploads an issue attachment.
func (f *Forgejo) Attach(ctx context.Context, number int, name string, data []byte) (string, error) {
	body, err := multipartFile("attachment", name, data)
	if err != nil {
		return "", err
	}
	var out struct {
		URL string `json:"browser_download_url"`
	}
	u := f.c.repoPath("/issues/%d/assets?name=%s", number, url.QueryEscape(name))
	if err := f.c.do(ctx, http.MethodPost, u, body, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("attachment %s: no URL in response", name)
	}
	return out.URL, nil
}

func (f *Forgejo) EnsureLabel(ctx context.Context, name, color, description string) error {
	return f.c.ensureLabel(ctx, name, "#"+strings.TrimPrefix(color, "#"), description, "limit")
}

func (f *Forgejo) State(ctx context.Context, number int, isPR bool) (State, error) {
	return f.c.state(ctx, number, isPR)
}

// FileURL and RawURL take a commit SHA or a branch name.
func (f *Forgejo) FileURL(ref, path string) string {
	return f.c.fileURL("/src/%s", forgejoRef(ref), path)
}
func (f *Forgejo) RawURL(ref, path string) string {
	return f.c.fileURL("/raw/%s", forgejoRef(ref), path)
}

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func forgejoRef(ref string) string {
	if commitSHA.MatchString(ref) {
		return "commit/" + ref
	}
	return "branch/" + ref
}

func (f *Forgejo) LastReviewComment(ctx context.Context, number int) (string, error) {
	return f.c.lastComment(ctx, number)
}

// ParseWebhook checks X-Forgejo-Signature (hex HMAC-SHA256, no prefix),
// falling back to the X-Gitea-Signature header older versions send.
func (f *Forgejo) ParseWebhook(h http.Header, body []byte) (Event, error) {
	sig := h.Get("X-Forgejo-Signature")
	if sig == "" {
		sig = h.Get("X-Gitea-Signature")
	}
	if !validMAC(f.c.secret, body, sig) {
		return Event{}, ErrSignature
	}
	event := h.Get("X-Forgejo-Event")
	if event == "" {
		event = h.Get("X-Gitea-Event")
	}
	return decodeEvent(event, body)
}

func validMAC(secret, body []byte, sigHex string) bool {
	if len(secret) == 0 || sigHex == "" {
		return false
	}
	want, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// Sign returns the hex HMAC-SHA256 of body, as both hosts compute it.
func Sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
