// Package gitrepo manages the local checkout of the content repository by
// running the git command.
package gitrepo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a local checkout.
type Repo struct {
	Dir string
}

// Ensure clones url into dir when dir has no checkout yet. With an empty
// url the directory is used as is, which suits local development.
func Ensure(ctx context.Context, dir, url, branch string) (*Repo, error) {
	r := &Repo{Dir: dir}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil || url == "" {
		return r, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("%s is not empty and is not a git checkout", dir)
	}
	args := []string{"clone", "--branch", branch, "--single-branch", url, dir}
	if _, err := run(ctx, "", args...); err != nil {
		return nil, err
	}
	return r, nil
}

// HeadSHA returns the commit currently checked out, or "" if dir is not a
// git checkout.
func (r *Repo) HeadSHA(ctx context.Context) string {
	out, err := run(ctx, r.Dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
