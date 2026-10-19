package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setup creates a bare "remote" with one commit on main and a second
// working clone used to push upstream changes.
func setup(t *testing.T) (remote, upstream string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	upstream = filepath.Join(root, "upstream")
	sh(t, root, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	sh(t, root, "clone", "--quiet", remote, upstream)
	os.MkdirAll(filepath.Join(upstream, "content"), 0o755)
	os.WriteFile(filepath.Join(upstream, "content", "a.md"), []byte("v1\n"), 0o644)
	sh(t, upstream, "add", ".")
	sh(t, upstream, "commit", "--quiet", "-m", "init")
	sh(t, upstream, "push", "--quiet", "origin", "HEAD:main")
	return remote, upstream
}

func TestCommitFromBaseAndSync(t *testing.T) {
	ctx := context.Background()
	remote, upstream := setup(t)
	dir := filepath.Join(t.TempDir(), "checkout")

	r, err := Ensure(ctx, dir, remote, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	base := r.HeadSHA(ctx)
	if base == "" {
		t.Fatal("no head after clone")
	}

	// Someone else changes main after the user opened the article.
	os.WriteFile(filepath.Join(upstream, "content", "a.md"), []byte("v2 upstream\n"), 0o644)
	sh(t, upstream, "commit", "--quiet", "-am", "upstream edit")
	sh(t, upstream, "push", "--quiet", "origin", "HEAD:main")

	commit, err := r.CommitAndPush(ctx, base, "suggest/1-a-abc", "Edit a",
		Author{"User", "u@x"}, Author{"bot", "b@x"},
		[]Change{{Path: "content/a.md", Data: []byte("v2 user\n")}, {Path: "content/assets/p.png", Data: []byte("png")}})
	if err != nil {
		t.Fatal(err)
	}
	if parent := sh(t, remote, "rev-parse", commit+"^"); parent != base {
		t.Errorf("parent = %s, want base %s", parent, base)
	}
	if got := sh(t, remote, "show", "suggest/1-a-abc:content/a.md"); got != "v2 user" {
		t.Errorf("branch content = %q", got)
	}
	if got := sh(t, remote, "show", "suggest/1-a-abc:content/assets/p.png"); got != "png" {
		t.Errorf("asset = %q", got)
	}
	if got := sh(t, remote, "log", "-1", "--format=%an <%ae> / %cn", commit); got != "User <u@x> / bot" {
		t.Errorf("author/committer = %q", got)
	}
	// The local working tree is untouched by the commit.
	if data, _ := os.ReadFile(filepath.Join(dir, "content", "a.md")); string(data) != "v1\n" {
		t.Errorf("working tree changed: %q", data)
	}

	head, err := r.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if head != sh(t, remote, "rev-parse", "main") {
		t.Errorf("sync head = %s", head)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "content", "a.md")); string(data) != "v2 upstream\n" {
		t.Errorf("after sync = %q", data)
	}
}

func TestEnsureChecksOrigin(t *testing.T) {
	ctx := context.Background()
	remote, upstream := setup(t)
	if _, err := Ensure(ctx, upstream, remote+"-other", "main", ""); err == nil {
		t.Error("foreign checkout accepted")
	}
	if _, err := Ensure(ctx, upstream, remote, "main", ""); err != nil {
		t.Errorf("own checkout rejected: %v", err)
	}
	unmanaged, err := Ensure(ctx, upstream, "", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unmanaged.Sync(ctx); err == nil {
		t.Error("unmanaged checkout synced")
	}
}

func TestAuthEnv(t *testing.T) {
	r := &Repo{URL: "https://example.com/o/r.git", token: "secret"}
	env := strings.Join(r.authEnv(), "\n")
	if !strings.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0") {
		t.Errorf("env = %s", env)
	}
	r.URL = "/local/path"
	if strings.Contains(strings.Join(r.authEnv(), "\n"), "Authorization") {
		t.Error("token sent to a non-HTTPS remote")
	}
}

func TestSymlinksArePlainFiles(t *testing.T) {
	ctx := context.Background()
	remote, upstream := setup(t)
	os.Symlink("/etc/hostname", filepath.Join(upstream, "content", "link.md"))
	sh(t, upstream, "add", ".")
	sh(t, upstream, "commit", "--quiet", "-m", "link")
	sh(t, upstream, "push", "--quiet", "origin", "HEAD:main")

	check := func(dir string) {
		t.Helper()
		p := filepath.Join(dir, "content", "link.md")
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("link.md = %v, %v; want a plain file", info, err)
		}
		if data, _ := os.ReadFile(p); string(data) != "/etc/hostname" {
			t.Errorf("link.md holds %q", data)
		}
	}
	// A fresh clone.
	dir := filepath.Join(t.TempDir(), "checkout")
	if _, err := Ensure(ctx, dir, remote, "main", ""); err != nil {
		t.Fatal(err)
	}
	check(dir)
	// A checkout from before, which had links on: Ensure turns them off,
	// so the next sync writes plain files.
	old := filepath.Join(t.TempDir(), "old")
	sh(t, filepath.Dir(old), "clone", "--quiet", remote, old)
	sh(t, old, "reset", "--quiet", "--hard", "HEAD~1")
	r, err := Ensure(ctx, old, remote, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	check(old)
}

func TestSyncRemovesStaleLocks(t *testing.T) {
	ctx := context.Background()
	remote, upstream := setup(t)
	dir := filepath.Join(t.TempDir(), "checkout")
	r, err := Ensure(ctx, dir, remote, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(upstream, "content", "a.md"), []byte("v2\n"), 0o644)
	sh(t, upstream, "commit", "--quiet", "-am", "v2")
	sh(t, upstream, "push", "--quiet", "origin", "HEAD:main")
	// A sync killed halfway left its lock behind.
	for _, name := range []string{"index.lock", "HEAD.lock"} {
		os.WriteFile(filepath.Join(dir, ".git", name), nil, 0o644)
	}
	if _, err := r.Sync(ctx); err != nil {
		t.Fatalf("sync with a stale lock: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "content", "a.md")); string(data) != "v2\n" {
		t.Errorf("after sync = %q", data)
	}
}

func TestCancelStopsGitGently(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sleep", "10")
	gently(cmd)
	if cmd.WaitDelay != gitGrace {
		t.Fatal("no grace")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cancel()
	err := cmd.Wait()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
		t.Errorf("wait = %v, want ended by SIGTERM", err)
	}
}
