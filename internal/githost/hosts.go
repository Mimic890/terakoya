package githost

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// GitHub uses the REST API with a fine-grained personal access token that
// has read and write access to contents and pull requests.
type GitHub struct{ c *client }

func (g *GitHub) OpenPR(ctx context.Context, pr NewPR) (PR, error) {
	return g.c.openPR(ctx, pr)
}

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

func (f *Forgejo) OpenPR(ctx context.Context, pr NewPR) (PR, error) {
	return f.c.openPR(ctx, pr)
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
