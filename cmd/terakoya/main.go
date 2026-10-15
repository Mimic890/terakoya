// Command terakoya runs the Telegram bot and validates content.
//
// Usage:
//
//	terakoya [serve] [-config file]
//	terakoya lint [-strict] <content-dir>
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mimic890/terakoya/internal/bot"
	"github.com/Mimic890/terakoya/internal/config"
	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/lint"
	"github.com/Mimic890/terakoya/internal/store"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "lint") {
		cmd, args = args[0], args[1:]
	}
	var code int
	switch cmd {
	case "lint":
		code = runLint(args)
	default:
		code = runServe(args)
	}
	os.Exit(code)
}

func runLint(args []string) int {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	strict := fs.Bool("strict", false, "treat warnings as errors")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: terakoya lint [-strict] <content-dir>")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	issues, err := lint.Run(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var errs, warns int
	for _, i := range issues {
		fmt.Println(i)
		if i.Severity == content.Error {
			errs++
		} else {
			warns++
		}
	}
	fmt.Fprintf(os.Stderr, "%d errors, %d warnings\n", errs, warns)
	if errs > 0 || (*strict && warns > 0) {
		return 1
	}
	return 0
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", os.Getenv("TERAKOYA_CONFIG"), "path to a YAML config file")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Error("data dir", "err", err)
		return 1
	}
	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		log.Error("open database", "err", err)
		return 1
	}
	defer st.Close()

	repo, err := gitrepo.Ensure(ctx, cfg.RepoDir, cfg.RepoURL, cfg.RepoBranch)
	if err != nil {
		log.Error("content repository", "err", err)
		return 1
	}
	b, err := bot.New(ctx, cfg, st, repo, log)
	if err != nil {
		log.Error("start bot", "err", err)
		return 1
	}
	b.Run(ctx)
	log.Info("stopped")
	return 0
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
