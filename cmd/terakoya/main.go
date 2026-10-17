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
	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/lint"
	"github.com/Mimic890/terakoya/internal/raster"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/webhook"
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
		fmt.Fprintln(fs.Output(), "usage: terakoya lint [-strict] <vault>")
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	ras := raster.New(os.Getenv("TERAKOYA_RESVG"), os.Getenv("TERAKOYA_FONTS_DIR"))
	issues, err := lint.Run(context.Background(), fs.Arg(0), ras)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var errs, warns, infos int
	for _, i := range issues {
		fmt.Println(i)
		switch i.Severity {
		case content.Error:
			errs++
		case content.Warning:
			warns++
		default:
			infos++
		}
	}
	fmt.Fprintf(os.Stderr, "%d errors, %d warnings, %d notes\n", errs, warns, infos)
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

	repo, err := gitrepo.Ensure(ctx, cfg.RepoDir, cfg.RepoURL, cfg.RepoBranch, cfg.GitToken)
	if err != nil {
		log.Error("content repository", "err", err)
		return 1
	}
	var host githost.Host
	if cfg.Suggestions() {
		host, err = githost.New(githost.Config{Kind: cfg.GitHost, APIURL: cfg.GitAPIURL, WebURL: cfg.RepoURL,
			Repo: cfg.GitRepo, Token: cfg.GitToken, Secret: cfg.GitWebhookSecret})
		if err != nil {
			log.Error("git host", "err", err)
			return 1
		}
		if err := host.EnsureLabel(ctx, cfg.AcceptedLabel, "#2da44e", "Error report fixed: counts toward the user's rank"); err != nil {
			log.Warn("could not create the accepted label; create it by hand", "label", cfg.AcceptedLabel, "err", err)
		}
	}
	b, err := bot.New(ctx, cfg, st, repo, host, log)
	if err != nil {
		log.Error("start bot", "err", err)
		return 1
	}
	if host != nil {
		h := webhook.Handler(ctx, host, b.HandleGitEvent, log)
		go func() {
			if err := webhook.Serve(ctx, cfg.HTTPAddr, h, log); err != nil {
				log.Error("http server", "err", err)
				stop()
			}
		}()
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
