// Package webhook serves the HTTP endpoints the git host calls.
package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Mimic890/terakoya/internal/githost"
)

// Path is where the git host delivers webhooks.
const Path = "/webhooks/git"

const maxBody = 5 << 20

// Handler verifies deliveries and passes events to handle. Deliveries are
// acknowledged before they are processed so the host does not time out;
// handle runs with ctx, which should outlive individual requests.
func Handler(ctx context.Context, host githost.Host, handle func(context.Context, githost.Event), log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST "+Path, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		ev, err := host.ParseWebhook(r.Header, body)
		switch {
		case errors.Is(err, githost.ErrSignature):
			log.Warn("webhook with invalid signature", "remote", r.RemoteAddr)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		case err != nil:
			log.Warn("bad webhook", "err", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		case ev.Kind == githost.Ignored:
			w.WriteHeader(http.StatusNoContent)
			return
		}
		log.Info("webhook", "kind", ev.Kind, "pr", ev.PRNumber, "ref", ev.Ref)
		w.WriteHeader(http.StatusAccepted)
		go func() {
			hctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			handle(hctx, ev)
		}()
	})
	return mux
}

// Serve runs an HTTP server on addr until ctx is cancelled.
func Serve(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second,
		// A delivery is at most maxBody and is answered before it is
		// handled; slow clients do not hold connections open.
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("http server listening", "addr", addr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}
