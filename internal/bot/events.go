package bot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/store"
)

// HandleGitEvent reacts to a verified webhook delivery from the git host.
func (b *Bot) HandleGitEvent(ctx context.Context, ev githost.Event) {
	switch ev.Kind {
	case githost.Push:
		if ev.Ref == "refs/heads/"+b.cfg.RepoBranch {
			b.Sync(ctx)
		}
	case githost.PRMerged, githost.PRClosed:
		b.resolveSuggestion(ctx, ev)
	}
}

func (b *Bot) resolveSuggestion(ctx context.Context, ev githost.Event) {
	sg, err := b.store.SuggestionByPR(ctx, ev.PRNumber)
	if errors.Is(err, store.ErrNotFound) {
		return // not opened by the bot
	}
	if err != nil {
		b.log.Error("find suggestion", "pr", ev.PRNumber, "err", err)
		return
	}
	if ev.HeadBranch != "" && ev.HeadBranch != sg.Branch {
		b.log.Warn("pull request branch does not match the suggestion", "pr", ev.PRNumber, "branch", ev.HeadBranch)
		return
	}
	status := store.StatusClosed
	if ev.Kind == githost.PRMerged {
		status = store.StatusMerged
	}
	changed, err := b.store.ResolveSuggestion(ctx, sg.ID, status)
	if err != nil {
		b.log.Error("resolve suggestion", "pr", ev.PRNumber, "err", err)
		return
	}
	if !changed {
		return // repeated delivery
	}
	b.log.Info("suggestion resolved", "pr", ev.PRNumber, "status", status, "user", sg.TgID)

	var text string
	if status == store.StatusMerged {
		u, err := b.store.User(ctx, sg.TgID)
		if err != nil {
			b.log.Error("load user", "err", err)
			return
		}
		rank := b.cfg.Ladder.For(u.MergedCount)
		text = fmt.Sprintf(b.t.Merged, sg.Title, u.MergedCount, rank.Title)
		if rank != b.cfg.Ladder.For(u.MergedCount-1) {
			text += "\n\n" + fmt.Sprintf(b.t.RankUp, rank.Title)
		}
	} else {
		text = fmt.Sprintf(b.t.Closed, sg.Title)
		comment, err := b.host.LastReviewComment(ctx, sg.PRNumber)
		if err != nil {
			b.log.Warn("read review comments", "pr", sg.PRNumber, "err", err)
		}
		if comment != "" {
			text += "\n\n" + fmt.Sprintf(b.t.ReviewComment, comment)
		}
	}
	// Users have a private chat with the bot, whose ID equals their user ID.
	b.send(ctx, sg.TgID, text, nil)
}

// Sync pulls the content repository and reloads the catalog if it changed.
func (b *Bot) Sync(ctx context.Context) {
	if !b.repo.Managed() {
		return
	}
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	sha, err := b.repo.Sync(ctx)
	if err != nil {
		b.log.Error("sync content", "err", err)
		return
	}
	b.mu.RLock()
	same := sha == b.sha
	b.mu.RUnlock()
	if same {
		return
	}
	if err := b.Reload(ctx); err != nil {
		b.log.Error("reload content", "err", err)
	}
}

// syncLoop pulls periodically as a fallback for missed push webhooks.
func (b *Bot) syncLoop(ctx context.Context) {
	if !b.repo.Managed() || b.cfg.SyncInterval <= 0 {
		return
	}
	t := time.NewTicker(b.cfg.SyncInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.Sync(ctx)
		}
	}
}
