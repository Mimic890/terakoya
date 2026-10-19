// Package gitrepo manages the local checkout of the content repository by
// running the git command.
package gitrepo

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repo is a local checkout.
type Repo struct {
	Dir    string
	URL    string
	Branch string
	token  string
	mu     sync.Mutex
}

// Ensure prepares the checkout in dir. With a URL, an empty dir is cloned
// and an existing checkout must have that URL as origin; the bot then owns
// the checkout and may reset it when syncing. Its checkout writes symbolic
// links as plain files holding the target, so a link committed to the
// vault cannot point at files on the server. Without a URL, dir is used as
// is and never modified, which suits local development.
func Ensure(ctx context.Context, dir, url, branch, token string) (*Repo, error) {
	r := &Repo{Dir: dir, URL: url, Branch: branch, token: token}
	if url == "" {
		return r, nil
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		origin, err := r.git(ctx, "remote", "get-url", "origin")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(origin) != url {
			return nil, fmt.Errorf("%s is a checkout of %s, not %s", dir, strings.TrimSpace(origin), url)
		}
		if _, err := r.git(ctx, "config", "core.symlinks", "false"); err != nil {
			return nil, err
		}
		return r, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty and is not a git checkout", dir)
	}
	if _, err := r.run(ctx, "", "clone", "--config", "core.symlinks=false", "--branch", branch, "--single-branch", url, dir); err != nil {
		return nil, err
	}
	return r, nil
}

// Managed reports whether the bot owns the checkout.
func (r *Repo) Managed() bool { return r.URL != "" }

// HeadSHA returns the commit currently checked out, or "" if dir is not a
// git checkout.
func (r *Repo) HeadSHA(ctx context.Context) string {
	out, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Sync fetches the branch and resets the checkout to it. It returns the
// new head commit.
func (r *Repo) Sync(ctx context.Context) (string, error) {
	if !r.Managed() {
		return "", fmt.Errorf("checkout is not managed by the bot; set TERAKOYA_REPO_URL")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.git(ctx, "fetch", "--quiet", "origin", r.Branch); err != nil {
		return "", err
	}
	if _, err := r.git(ctx, "reset", "--quiet", "--hard", "FETCH_HEAD"); err != nil {
		return "", err
	}
	return r.HeadSHA(ctx), nil
}

// Change is a file to write in a commit, by path relative to the
// repository root.
type Change struct {
	Path string
	Data []byte
}

// Author of a commit.
type Author struct {
	Name, Email string
}

// CommitAndPush creates a commit on top of base that writes the changes,
// and pushes it as a new branch, such as the pictures of a report on a
// host without issue attachments. The working tree is not touched, so the
// commit is based on exactly the content the user saw.
func (r *Repo) CommitAndPush(ctx context.Context, base, branch, message string, author, committer Author, changes []Change) (string, error) {
	if !r.Managed() {
		return "", fmt.Errorf("checkout is not managed by the bot; set TERAKOYA_REPO_URL")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	idx, err := os.CreateTemp("", "terakoya-index-*")
	if err != nil {
		return "", err
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := []string{"GIT_INDEX_FILE=" + idx.Name()}

	if _, err := r.gitEnv(ctx, env, nil, "read-tree", base); err != nil {
		return "", err
	}
	for _, c := range changes {
		blob, err := r.gitEnv(ctx, nil, c.Data, "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		info := "100644," + strings.TrimSpace(blob) + "," + c.Path
		if _, err := r.gitEnv(ctx, env, nil, "update-index", "--add", "--cacheinfo", info); err != nil {
			return "", err
		}
	}
	tree, err := r.gitEnv(ctx, env, nil, "write-tree")
	if err != nil {
		return "", err
	}
	commitEnv := []string{
		"GIT_AUTHOR_NAME=" + author.Name, "GIT_AUTHOR_EMAIL=" + author.Email,
		"GIT_COMMITTER_NAME=" + committer.Name, "GIT_COMMITTER_EMAIL=" + committer.Email,
	}
	commit, err := r.gitEnv(ctx, commitEnv, []byte(message), "commit-tree", strings.TrimSpace(tree), "-p", base)
	if err != nil {
		return "", err
	}
	commit = strings.TrimSpace(commit)
	if _, err := r.git(ctx, "push", "--quiet", "origin", commit+":refs/heads/"+branch); err != nil {
		return "", err
	}
	return commit, nil
}

func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	return r.gitEnv(ctx, nil, nil, args...)
}

func (r *Repo) gitEnv(ctx context.Context, env []string, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(append(os.Environ(), r.authEnv()...), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return output(cmd, args[0])
}

func (r *Repo) run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), r.authEnv()...)
	return output(cmd, args[0])
}

// authEnv passes the token as an HTTP header through environment config,
// so it never appears in command lines or in .git/config.
func (r *Repo) authEnv() []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if r.token == "" || !strings.HasPrefix(r.URL, "https://") {
		return env
	}
	cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + r.token))
	return append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic "+cred,
	)
}

func output(cmd *exec.Cmd, name string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// CommitTime returns when a commit was made, or the zero time if it
// cannot be read.
func (r *Repo) CommitTime(ctx context.Context, sha string) time.Time {
	if sha == "" {
		return time.Time{}
	}
	out, err := r.git(ctx, "log", "-1", "--format=%ct", sha)
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
