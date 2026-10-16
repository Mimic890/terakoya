package webhook

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
