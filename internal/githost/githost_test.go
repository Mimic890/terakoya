package githost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		kind, auth, issuePath string
	}{
		{"github", "Bearer tok", "/repos/o/r/issues"},
		{"forgejo", "token tok", "/api/v1/repos/o/r/issues"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			var rec recorded
			srv := server(t, &rec)
			h, err := New(Config{Kind: c.kind, APIURL: srv.URL, Repo: "o/r", Token: "tok", Secret: "s"})
			if err != nil {
				t.Fatal(err)
			}
			issue, err := h.OpenIssue(context.Background(), "T", "B")
			if err != nil {
				t.Fatal(err)
			}
			if issue.Number != 7 || issue.URL == "" {
				t.Errorf("issue = %+v", issue)
			}
			if rec.auth != c.auth || rec.path != c.issuePath {
				t.Errorf("auth %q path %q", rec.auth, rec.path)
			}
			if rec.body["title"] != "T" || rec.body["body"] != "B" {
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
	issueClosed := []byte(`{"action":"closed","issue":{"number":12,"state":"closed","closed_at":"2026-01-02T10:00:00Z","labels":[{"name":"Accepted"}]}}`)
	issueLabeled := []byte(`{"action":"label_updated","issue":{"number":12,"state":"open","labels":[]}}`)
	issueOpened := []byte(`{"action":"opened","issue":{"number":12,"state":"open"}}`)
	prAsIssue := []byte(`{"action":"closed","issue":{"number":7,"state":"closed","pull_request":{"merged":true}}}`)

	gh, _ := New(Config{Kind: "github", Repo: "o/r", Secret: string(secret)})
	fj, _ := New(Config{Kind: "forgejo", APIURL: "https://git.example", Repo: "o/r", Secret: string(secret)})
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
			ev, err = c.h.ParseWebhook(c.header("issues", issueClosed), issueClosed)
			if err != nil || ev.Kind != IssueChanged || ev.PRNumber != 12 || !ev.State.Closed || !ev.State.HasLabel("accepted") || ev.State.ClosedAt.IsZero() {
				t.Errorf("issue closed = %+v, %v", ev, err)
			}
			ev, err = c.h.ParseWebhook(c.header("issues", issueLabeled), issueLabeled)
			if err != nil || ev.Kind != IssueChanged || ev.State.Closed {
				t.Errorf("issue labeled = %+v, %v", ev, err)
			}
			for _, body := range [][]byte{issueOpened, prAsIssue} {
				if ev, err := c.h.ParseWebhook(c.header("issues", body), body); err != nil || ev.Kind != Ignored {
					t.Errorf("ignored issue event = %+v, %v", ev, err)
				}
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
	h, _ := New(Config{Kind: "github", Repo: "o/r"})
	body := []byte(`{}`)
	header := http.Header{"X-Github-Event": {"push"}, "X-Hub-Signature-256": {"sha256=" + Sign(nil, body)}}
	if _, err := h.ParseWebhook(header, body); !errors.Is(err, ErrSignature) {
		t.Errorf("err = %v", err)
	}
}

func TestIssues(t *testing.T) {
	type req struct {
		method, path, query, contentType string
		body                             []byte
	}
	var reqs []req
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqs = append(reqs, req{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), body})
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		switch {
		case r.Method == http.MethodPost && p == "/repos/o/r/issues":
			w.Write([]byte(`{"number": 12, "html_url": "https://host/o/r/issues/12"}`))
		case r.Method == http.MethodPost && p == "/repos/o/r/issues/12/assets":
			w.Write([]byte(`{"browser_download_url": "https://host/attachments/abc"}`))
		case r.Method == http.MethodGet && p == "/repos/o/r/labels":
			if strings.Contains(r.URL.RawQuery, "page=1") {
				w.Write([]byte(`[{"name": "bug"}]`))
			} else {
				w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodGet && p == "/repos/o/r/issues/12":
			w.Write([]byte(`{"number": 12, "state": "closed", "closed_at": "2026-01-02T10:00:00Z", "labels": [{"name": "accepted"}]}`))
		case r.Method == http.MethodGet && p == "/repos/o/r/pulls/7":
			w.Write([]byte(`{"number": 7, "state": "closed", "merged": true}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	for _, kind := range []string{"forgejo", "github"} {
		reqs = nil
		h, _ := New(Config{Kind: kind, APIURL: srv.URL, WebURL: "https://host/o/r.git", Repo: "o/r", Token: "tok"})
		is, err := h.OpenIssue(ctx, "Error report: X", "body")
		if err != nil || is.Number != 12 {
			t.Fatalf("%s: issue = %+v, %v", kind, is, err)
		}
		if err := h.EditIssue(ctx, 12, "new body"); err != nil || reqs[1].method != http.MethodPatch || string(reqs[1].body) != `{"body":"new body"}` {
			t.Errorf("%s: edit = %v %+v", kind, err, reqs[1])
		}
		u, err := h.Attach(ctx, 12, "x 1.jpg", []byte("JPEG"))
		if kind == "github" {
			if !errors.Is(err, ErrUnsupported) {
				t.Errorf("github attach: %v", err)
			}
		} else if r := reqs[len(reqs)-1]; err != nil || u != "https://host/attachments/abc" || r.query != "name=x+1.jpg" ||
			!strings.HasPrefix(r.contentType, "multipart/form-data") || !strings.Contains(string(r.body), `name="attachment"; filename="x 1.jpg"`) {
			t.Errorf("forgejo attach = %q, %v, %+v", u, err, r)
		}

		reqs = nil
		if err := h.EnsureLabel(ctx, "accepted", "#2da44e", "Counts toward the rank"); err != nil {
			t.Fatal(err)
		}
		last := reqs[len(reqs)-1]
		color := `"color":"#2da44e"`
		if kind == "github" {
			color = `"color":"2da44e"`
		}
		if last.method != http.MethodPost || !strings.HasSuffix(last.path, "/repos/o/r/labels") || !strings.Contains(string(last.body), color) {
			t.Errorf("%s: label not created: %+v", kind, last)
		}

		st, err := h.State(ctx, 12, false)
		if err != nil || !st.Closed || !st.HasLabel("accepted") || st.Merged || st.ClosedAt.IsZero() {
			t.Errorf("%s: issue state = %+v, %v", kind, st, err)
		}
		st, err = h.State(ctx, 7, true)
		if err != nil || !st.Closed || !st.Merged {
			t.Errorf("%s: PR state = %+v, %v", kind, st, err)
		}
	}

	fj, _ := New(Config{Kind: "forgejo", APIURL: "https://host", WebURL: "https://host/o/r.git", Repo: "o/r"})
	sha := strings.Repeat("ab", 20)
	if got := fj.FileURL(sha, "content/7/Тема.md"); got != "https://host/o/r/src/commit/"+sha+"/content/7/%D0%A2%D0%B5%D0%BC%D0%B0.md" {
		t.Errorf("forgejo file URL = %s", got)
	}
	if got := fj.RawURL("suggest/1-a-b", "content/x.png"); got != "https://host/o/r/raw/branch/suggest/1-a-b/content/x.png" {
		t.Errorf("forgejo raw URL = %s", got)
	}
	gh, _ := New(Config{Kind: "github", WebURL: "https://github.com/o/r", Repo: "o/r"})
	if got := gh.RawURL("main", "content/x.png"); got != "https://github.com/o/r/raw/main/content/x.png" {
		t.Errorf("github raw URL = %s", got)
	}
}
