package adminapi

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/bot"
)

// The bot is what the socket serves.
var _ Service = (*bot.Bot)(nil)

func TestListen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		t.Errorf("socket = %v, %v", info.Mode(), err)
	}
	// A bot still answering keeps its socket.
	go func(l net.Listener) {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}(l)
	if _, err := Listen(path); err == nil {
		t.Error("took a socket in use")
	}
	l.Close()
	// A stale one, left by a bot that is gone, is replaced.
	os.Remove(path)
	stale, _ := net.Listen("unix", path)
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	stale.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no stale socket: %v", err)
	}
	l, err = Listen(path)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	l.Close()
}

func TestServeRemovesSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Serve(ctx, path, nil, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket left: %v", err)
	}
}
