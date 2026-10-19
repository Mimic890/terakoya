// Package stats measures the bot for /admin: traffic to the Telegram API,
// disk use, memory and the container's network counters.
package stats

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/Mimic890/terakoya/internal/store"
)

// Counter is an http.RoundTripper that counts calls and the bytes of
// request and response bodies.
type Counter struct {
	Next http.RoundTripper // http.DefaultTransport if nil

	calls, sent, recv                atomic.Int64 // since start
	takenCalls, takenSent, takenRecv atomic.Int64 // already handed out by Take
}

// RoundTrip counts and passes the request on.
func (c *Counter) RoundTrip(r *http.Request) (*http.Response, error) {
	next := c.Next
	if next == nil {
		next = http.DefaultTransport
	}
	c.calls.Add(1)
	if r.Body != nil {
		r.Body = &countingBody{ReadCloser: r.Body, n: &c.sent}
	}
	resp, err := next.RoundTrip(r)
	if resp != nil && resp.Body != nil {
		resp.Body = &countingBody{ReadCloser: resp.Body, n: &c.recv}
	}
	return resp, err
}

type countingBody struct {
	io.ReadCloser
	n *atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n.Add(int64(n))
	return n, err
}

// Total is the traffic since start.
func (c *Counter) Total() store.Traffic {
	return store.Traffic{Calls: c.calls.Load(), Sent: c.sent.Load(), Received: c.recv.Load()}
}

// Take returns the traffic since the previous Take, to be stored.
func (c *Counter) Take() store.Traffic {
	t := c.Total()
	return store.Traffic{
		Calls:    t.Calls - c.takenCalls.Swap(t.Calls),
		Sent:     t.Sent - c.takenSent.Swap(t.Sent),
		Received: t.Received - c.takenRecv.Swap(t.Received),
	}
}

// Pending is the traffic Take has not handed out yet.
func (c *Counter) Pending() store.Traffic {
	t := c.Total()
	return store.Traffic{Calls: t.Calls - c.takenCalls.Load(), Sent: t.Sent - c.takenSent.Load(),
		Received: t.Received - c.takenRecv.Load()}
}

// Vault is the disk use of the vault checkout.
type Vault struct {
	Total, Markdown, Images, Git, Other int64
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".svg": true, ".gif": true, ".webp": true}

// VaultSize adds up the files of a vault checkout: notes, pictures and
// the .git directory.
func VaultSize(dir string) (Vault, error) {
	var v Vault
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		n := info.Size()
		v.Total += n
		rel, _ := filepath.Rel(dir, p)
		switch {
		case rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)):
			v.Git += n
		case strings.EqualFold(filepath.Ext(p), ".md"):
			v.Markdown += n
		case imageExts[strings.ToLower(filepath.Ext(p))]:
			v.Images += n
		default:
			v.Other += n
		}
		return nil
	})
	return v, err
}

// DirSize adds up every file under dir.
func DirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, err
}

// FilesSize adds up the files that exist among paths.
func FilesSize(paths ...string) int64 {
	var total int64
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			total += info.Size()
		}
	}
	return total
}

// RSS reads the resident memory of the process from a /proc status file,
// such as /proc/self/status.
func RSS(statusPath string) (int64, error) {
	f, err := os.Open(statusPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				break
			}
			kb, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				return 0, err
			}
			return kb << 10, nil
		}
	}
	return 0, fmt.Errorf("%s: no VmRSS", statusPath)
}

// Network sums the received and sent bytes of every interface but lo,
// from a directory such as /sys/class/net. In a container they count
// since it started.
func Network(root string) (rx, tx int64, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if e.Name() == "lo" {
			continue
		}
		rx += readInt(filepath.Join(root, e.Name(), "statistics", "rx_bytes"))
		tx += readInt(filepath.Join(root, e.Name(), "statistics", "tx_bytes"))
	}
	return rx, tx, nil
}

func readInt(p string) int64 {
	data, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return n
}

// Bytes formats a size the Russian way: «512 Б», «1,5 КБ», «12,3 МБ».
func Bytes(n int64) string {
	units := []string{"Б", "КБ", "МБ", "ГБ", "ТБ"}
	if n < 1024 {
		return fmt.Sprintf("%d %s", n, units[0])
	}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	s := strconv.FormatFloat(f, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return strings.Replace(s, ".", ",", 1) + " " + units[i]
}
