package stats

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/store"
)

func write(t *testing.T, root, rel string, size int) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCounter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	c := &Counter{}
	client := &http.Client{Transport: c}
	for i := 0; i < 2; i++ {
		resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("hello"))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if got := c.Total(); got != (store.Traffic{Calls: 2, Sent: 10, Received: 200}) {
		t.Errorf("total = %+v", got)
	}
	if got := c.Take(); got != (store.Traffic{Calls: 2, Sent: 10, Received: 200}) {
		t.Errorf("take = %+v", got)
	}
	if got := c.Take(); got != (store.Traffic{}) {
		t.Errorf("second take = %+v", got)
	}
	resp, _ := client.Get(srv.URL)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got := c.Pending(); got != (store.Traffic{Calls: 1, Received: 100}) {
		t.Errorf("pending = %+v", got)
	}
	if got := c.Total(); got.Calls != 3 {
		t.Errorf("total after = %+v", got)
	}
}

func TestVaultSize(t *testing.T) {
	root := t.TempDir()
	write(t, root, "Геометрия/7 класс/01 Тема/Тема.md", 100)
	write(t, root, "Геометрия/7 класс/01 Тема/_assets/a.PNG", 2000)
	write(t, root, "Геометрия/7 класс/01 Тема/_assets/b.svg", 300)
	write(t, root, ".git/objects/pack/p.pack", 5000)
	write(t, root, ".gitignore", 7)
	write(t, root, "README.MD", 10)
	v, err := VaultSize(root)
	if err != nil {
		t.Fatal(err)
	}
	if v != (Vault{Total: 7417, Markdown: 110, Images: 2300, Git: 5000, Other: 7}) {
		t.Errorf("vault = %+v", v)
	}
	if n, _ := DirSize(filepath.Join(root, ".git")); n != 5000 {
		t.Errorf("dir = %d", n)
	}
	if n := FilesSize(filepath.Join(root, ".gitignore"), filepath.Join(root, "missing")); n != 7 {
		t.Errorf("files = %d", n)
	}
}

func TestProcAndNetwork(t *testing.T) {
	root := t.TempDir()
	status := filepath.Join(root, "status")
	os.WriteFile(status, []byte("Name:\tterakoya\nVmPeak:\t  99999 kB\nVmRSS:\t   20480 kB\nThreads:\t9\n"), 0o644)
	if n, err := RSS(status); err != nil || n != 20480<<10 {
		t.Errorf("rss = %d, %v", n, err)
	}
	os.WriteFile(status, []byte("Name:\tx\n"), 0o644)
	if _, err := RSS(status); err == nil {
		t.Error("no VmRSS accepted")
	}

	net := filepath.Join(root, "net")
	for iface, v := range map[string][2]string{"lo": {"999", "999"}, "eth0": {"1000\n", "200\n"}, "eth1": {"5", "7"}} {
		write(t, net, iface+"/statistics/rx_bytes", 0)
		os.WriteFile(filepath.Join(net, iface, "statistics", "rx_bytes"), []byte(v[0]), 0o644)
		os.WriteFile(filepath.Join(net, iface, "statistics", "tx_bytes"), []byte(v[1]), 0o644)
	}
	if rx, tx, err := Network(net); err != nil || rx != 1005 || tx != 207 {
		t.Errorf("network = %d %d %v", rx, tx, err)
	}
}

func TestBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 Б", 512: "512 Б", 1024: "1 КБ", 1536: "1,5 КБ", 12900000: "12,3 МБ",
		3 << 30: "3 ГБ"} {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}
