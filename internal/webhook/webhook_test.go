package webhook

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/githost"
)

func TestHandler(t *testing.T) {
	secret := "s"
	host, err := githost.New(githost.Config{Kind: "forgejo", APIURL: "https://git.example", Repo: "o/r", Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan githost.Event, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := Handler(context.Background(), host, func(_ context.Context, ev githost.Event) { got <- ev }, log)

	post := func(event string, body []byte, sig string) int {
		req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(body))
		req.Header.Set("X-Forgejo-Event", event)
		req.Header.Set("X-Forgejo-Signature", sig)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	push := []byte(`{"ref":"refs/heads/main"}`)
	if code := post("push", push, githost.Sign([]byte(secret), push)); code != http.StatusAccepted {
		t.Fatalf("signed push: %d", code)
	}
	select {
	case ev := <-got:
		if ev.Kind != githost.Push || ev.Ref != "refs/heads/main" {
			t.Errorf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("handler not called")
	}

	if code := post("push", push, "00"); code != http.StatusUnauthorized {
		t.Errorf("bad signature: %d", code)
	}
	issue := []byte(`{"action":"opened"}`)
	if code := post("issues", issue, githost.Sign([]byte(secret), issue)); code != http.StatusNoContent {
		t.Errorf("ignored event: %d", code)
	}
	select {
	case ev := <-got:
		t.Errorf("unexpected event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz: %d", rec.Code)
	}
}

func TestHandlerPanicIsRecovered(t *testing.T) {
	secret := "s"
	host, _ := githost.New(githost.Config{Kind: "forgejo", APIURL: "https://git.example", Repo: "o/r", Secret: secret})
	var logs bytes.Buffer
	var mu sync.Mutex
	log := slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &mu}, nil))
	done := make(chan struct{}, 2)
	calls := 0
	h := Handler(context.Background(), host, func(context.Context, githost.Event) {
		defer func() { done <- struct{}{} }()
		calls++
		if calls == 1 {
			panic("boom")
		}
	}, log)
	push := []byte(`{"ref":"refs/heads/main"}`)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(push))
		req.Header.Set("X-Forgejo-Event", "push")
		req.Header.Set("X-Forgejo-Signature", githost.Sign([]byte(secret), push))
		h.ServeHTTP(httptest.NewRecorder(), req)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("handler not called")
		}
	}
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(logs.String(), `msg="panic recovered" in=webhook panic=boom`) {
		t.Errorf("log:\n%s", logs.String())
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
