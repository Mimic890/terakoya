package githost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recorded struct {
	auth, path string
	body       map[string]string
}

func server(t *testing.T, rec *recorded) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.auth, rec.path = r.Header.Get("Authorization"), r.URL.Path
		switch {
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&rec.body)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"number": 7, "html_url": "https://host/o/r/pull/7"}`))
		case r.URL.Path == "/api/v1/repos/o/r/pulls/7/reviews", r.URL.Path == "/repos/o/r/pulls/7/reviews":
			w.Write([]byte(`[{"body": "older review", "submitted_at": "2026-01-01T10:00:00Z"},
				{"body": "", "submitted_at": "2026-01-03T10:00:00Z"}]`))
		case r.URL.Path == "/api/v1/repos/o/r/issues/7/comments", r.URL.Path == "/repos/o/r/issues/7/comments":
			w.Write([]byte(`[{"body": "  please fix the formula  ", "created_at": "2026-01-02T10:00:00Z"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHosts(t *testing.T) {
	cases := []struct {
		kind, auth, prPath string
	}{
		{"github", "Bearer tok", "/repos/o/r/pulls"},
		{"forgejo", "token tok", "/api/v1/repos/o/r/pulls"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			var rec recorded
			srv := server(t, &rec)
			h, err := New(c.kind, srv.URL, "o/r", "tok", "s")
			if err != nil {
				t.Fatal(err)
			}
			pr, err := h.OpenPR(context.Background(), NewPR{Title: "T", Body: "B", Head: "suggest/x", Base: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if pr.Number != 7 || pr.URL == "" {
				t.Errorf("pr = %+v", pr)
			}
			if rec.auth != c.auth || rec.path != c.prPath {
				t.Errorf("auth %q path %q", rec.auth, rec.path)
			}
			if rec.body["head"] != "suggest/x" || rec.body["base"] != "main" || rec.body["title"] != "T" {
				t.Errorf("body = %v", rec.body)
			}
			got, err := h.LastReviewComment(context.Background(), 7)
			if err != nil || got != "please fix the formula" {
				t.Errorf("last comment = %q, %v", got, err)
			}
		})
	}
}

func TestWebhooks(t *testing.T) {
	secret := []byte("s3cret")
	merged := []byte(`{"action":"closed","number":7,"pull_request":{"number":7,"merged":true,"head":{"ref":"suggest/1-a-x"}}}`)
	closed := []byte(`{"action":"closed","pull_request":{"number":8,"merged":false,"head":{"ref":"suggest/1-b-y"}}}`)
	opened := []byte(`{"action":"opened","pull_request":{"number":9}}`)
	push := []byte(`{"ref":"refs/heads/main"}`)

	gh, _ := New("github", "", "o/r", "", string(secret))
	fj, _ := New("forgejo", "https://git.example", "o/r", "", string(secret))
	ghHeader := func(event string, body []byte) http.Header {
		return http.Header{"X-Github-Event": {event}, "X-Hub-Signature-256": {"sha256=" + Sign(secret, body)}}
	}
	fjHeader := func(event string, body []byte) http.Header {
		return http.Header{"X-Forgejo-Event": {event}, "X-Forgejo-Signature": {Sign(secret, body)}}
	}
	for name, c := range map[string]struct {
		h      Host
		header func(string, []byte) http.Header
	}{"github": {gh, ghHeader}, "forgejo": {fj, fjHeader}} {
		t.Run(name, func(t *testing.T) {
			ev, err := c.h.ParseWebhook(c.header("pull_request", merged), merged)
			if err != nil || ev.Kind != PRMerged || ev.PRNumber != 7 || ev.HeadBranch != "suggest/1-a-x" {
				t.Errorf("merged = %+v, %v", ev, err)
			}
			ev, err = c.h.ParseWebhook(c.header("pull_request", closed), closed)
			if err != nil || ev.Kind != PRClosed || ev.PRNumber != 8 {
				t.Errorf("closed = %+v, %v", ev, err)
			}
			ev, err = c.h.ParseWebhook(c.header("pull_request", opened), opened)
			if err != nil || ev.Kind != Ignored {
				t.Errorf("opened = %+v, %v", ev, err)
			}
			ev, err = c.h.ParseWebhook(c.header("push", push), push)
			if err != nil || ev.Kind != Push || ev.Ref != "refs/heads/main" {
				t.Errorf("push = %+v, %v", ev, err)
			}
			bad := c.header("push", push)
			if _, err := c.h.ParseWebhook(bad, []byte(`{"ref":"refs/heads/evil"}`)); !errors.Is(err, ErrSignature) {
				t.Errorf("tampered body accepted: %v", err)
			}
			if _, err := c.h.ParseWebhook(http.Header{}, push); !errors.Is(err, ErrSignature) {
				t.Errorf("unsigned delivery accepted: %v", err)
			}
		})
	}
}

func TestEmptySecretRejectsEverything(t *testing.T) {
	h, _ := New("github", "", "o/r", "", "")
	body := []byte(`{}`)
	header := http.Header{"X-Github-Event": {"push"}, "X-Hub-Signature-256": {"sha256=" + Sign(nil, body)}}
	if _, err := h.ParseWebhook(header, body); !errors.Is(err, ErrSignature) {
		t.Errorf("err = %v", err)
	}
}
