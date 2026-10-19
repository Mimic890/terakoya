// Package adminapi serves the bot's admin functions as JSON over a unix
// socket, for `terakoya admin`. There is no token: whoever may open the
// socket file, which only its owner can, is an admin.
package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/Mimic890/terakoya/internal/bot"
)

// Service is what the socket serves; *bot.Bot implements it.
type Service interface {
	AdminStatus(ctx context.Context) (bot.AdminStatus, error)
	AdminServer(ctx context.Context) (bot.AdminServer, error)
	AdminStats(ctx context.Context) (bot.AdminStats, error)
	AdminUsers(ctx context.Context, query string, page int) (bot.AdminUsers, error)
	AdminUser(ctx context.Context, ref string) (bot.AdminUser, error)
	ResetNickname(ctx context.Context, ref string, by int64) (string, bot.AdminUser, error)
	SyncNow(ctx context.Context) bot.SyncResult
	RecipientCount(ctx context.Context) (int, error)
	Broadcast(ctx context.Context, text string, photo []byte) (bot.BroadcastStarted, error)
}

// Limits of a request.
const (
	requestTimeout = 3 * time.Minute
	maxBody        = 20 << 20 // a broadcast picture
)

// NickReset is the answer to POST /nick-reset.
type NickReset struct {
	Old  string        `json:"old"`
	User bot.AdminUser `json:"user"`
}

// BroadcastRequest is the body of POST /broadcast.
type BroadcastRequest struct {
	Text  string `json:"text"`
	Photo []byte `json:"photo,omitempty"`
}

type refRequest struct {
	Ref string `json:"ref"`
}

type apiError struct {
	Error string `json:"error"`
}

// Listen opens the socket at path, readable and writable by its owner
// only. A socket left by a bot that is gone is removed first; one that
// still answers is refused.
func Listen(path string) (net.Listener, error) {
	if _, err := os.Lstat(path); err == nil {
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("%s is in use by another bot", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// Serve answers on the socket at path until ctx ends, then removes it.
func Serve(ctx context.Context, path string, svc Service, log *slog.Logger) error {
	l, err := Listen(path)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: Handler(svc, log), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
		os.Remove(path)
	}()
	log.Info("admin socket listening", "path", path)
	if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler routes the API.
func Handler(svc Service, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	handle := func(pattern string, f func(ctx context.Context, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
			defer cancel()
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			out, err := f(ctx, r)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				code := http.StatusInternalServerError
				var bad badRequest
				switch {
				case errors.Is(err, bot.ErrNoUser):
					code = http.StatusNotFound
				case errors.Is(err, bot.ErrNoNickname):
					code = http.StatusConflict
				case errors.As(err, &bad):
					code = http.StatusBadRequest
				}
				if code == http.StatusInternalServerError {
					log.Error("admin socket", "path", r.URL.Path, "err", err)
				}
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(apiError{err.Error()})
				return
			}
			json.NewEncoder(w).Encode(out)
		})
	}
	handle("GET /status", func(ctx context.Context, _ *http.Request) (any, error) { return svc.AdminStatus(ctx) })
	handle("GET /server", func(ctx context.Context, _ *http.Request) (any, error) { return svc.AdminServer(ctx) })
	handle("GET /stats", func(ctx context.Context, _ *http.Request) (any, error) { return svc.AdminStats(ctx) })
	handle("GET /users", func(ctx context.Context, r *http.Request) (any, error) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		return svc.AdminUsers(ctx, r.URL.Query().Get("search"), page)
	})
	handle("GET /user", func(ctx context.Context, r *http.Request) (any, error) {
		ref := r.URL.Query().Get("ref")
		if ref == "" {
			return nil, badRequest("ref is required")
		}
		return svc.AdminUser(ctx, ref)
	})
	handle("POST /nick-reset", func(ctx context.Context, r *http.Request) (any, error) {
		var req refRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Ref == "" {
			return nil, badRequest("ref is required")
		}
		old, u, err := svc.ResetNickname(ctx, req.Ref, 0)
		return NickReset{Old: old, User: u}, err
	})
	handle("POST /sync", func(ctx context.Context, _ *http.Request) (any, error) { return svc.SyncNow(ctx), nil })
	handle("GET /recipients", func(ctx context.Context, _ *http.Request) (any, error) {
		n, err := svc.RecipientCount(ctx)
		return map[string]int{"recipients": n}, err
	})
	handle("POST /broadcast", func(ctx context.Context, r *http.Request) (any, error) {
		var req BroadcastRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, badRequest("bad broadcast: " + err.Error())
		}
		if req.Text == "" && len(req.Photo) == 0 {
			return nil, badRequest("text or photo is required")
		}
		return svc.Broadcast(ctx, req.Text, req.Photo)
	})
	return mux
}

type badRequest string

func (e badRequest) Error() string { return string(e) }

// Client talks to the socket.
type Client struct {
	http *http.Client
}

// Dial returns a client for the socket at path.
func Dial(path string) *Client {
	return &Client{http: &http.Client{
		Timeout: requestTimeout + 10*time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}},
	}}
}

// call sends a request and decodes the answer into out; an error answer
// becomes the returned error.
func (c *Client) call(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	u := "http://admin" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach the bot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e apiError
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status(ctx context.Context) (s bot.AdminStatus, err error) {
	return s, c.call(ctx, http.MethodGet, "/status", nil, nil, &s)
}

func (c *Client) Server(ctx context.Context) (s bot.AdminServer, err error) {
	return s, c.call(ctx, http.MethodGet, "/server", nil, nil, &s)
}

func (c *Client) Stats(ctx context.Context) (s bot.AdminStats, err error) {
	return s, c.call(ctx, http.MethodGet, "/stats", nil, nil, &s)
}

func (c *Client) Users(ctx context.Context, search string, page int) (u bot.AdminUsers, err error) {
	q := url.Values{"page": {strconv.Itoa(page)}}
	if search != "" {
		q.Set("search", search)
	}
	return u, c.call(ctx, http.MethodGet, "/users", q, nil, &u)
}

func (c *Client) User(ctx context.Context, ref string) (u bot.AdminUser, err error) {
	return u, c.call(ctx, http.MethodGet, "/user", url.Values{"ref": {ref}}, nil, &u)
}

func (c *Client) NickReset(ctx context.Context, ref string) (r NickReset, err error) {
	return r, c.call(ctx, http.MethodPost, "/nick-reset", nil, refRequest{ref}, &r)
}

func (c *Client) Sync(ctx context.Context) (r bot.SyncResult, err error) {
	return r, c.call(ctx, http.MethodPost, "/sync", nil, struct{}{}, &r)
}

func (c *Client) Recipients(ctx context.Context) (int, error) {
	var out map[string]int
	err := c.call(ctx, http.MethodGet, "/recipients", nil, nil, &out)
	return out["recipients"], err
}

func (c *Client) Broadcast(ctx context.Context, text string, photo []byte) (r bot.BroadcastStarted, err error) {
	return r, c.call(ctx, http.MethodPost, "/broadcast", nil, BroadcastRequest{Text: text, Photo: photo}, &r)
}
