package bot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/store"
)

// rejectGrace is how long an error report must stay closed without the
// accepted label before the user is told it was not accepted. It lets the
// reviewer close first and label right after without a wrong message.
const rejectGrace = 2 * time.Minute

// HandleGitEvent reacts to a verified webhook delivery from the git host.
func (b *Bot) HandleGitEvent(ctx context.Context, ev githost.Event) {
	switch ev.Kind {
	case githost.Push:
		if ev.Ref == "refs/heads/"+b.cfg.RepoBranch {
			b.Sync(ctx)
		}
	case githost.PRMerged, githost.PRClosed:
		sg, ok := b.suggestionFor(ctx, ev.PRNumber)
		if !ok || sg.Kind == store.KindReport {
			return
		}
		if ev.HeadBranch != "" && ev.HeadBranch != sg.Branch {
			b.log.Warn("pull request branch does not match the suggestion", "pr", ev.PRNumber, "branch", ev.HeadBranch)
			return
		}
		b.resolve(ctx, sg, ev.Kind == githost.PRMerged)
	case githost.IssueChanged:
		sg, ok := b.suggestionFor(ctx, ev.PRNumber)
		if !ok || sg.Kind != store.KindReport || !ev.State.Closed {
			return
		}
		if ev.State.HasLabel(b.cfg.AcceptedLabel) {
			b.resolve(ctx, sg, true)
			return
		}
		if sg.Status == store.StatusOpen {
			// Decide later, in case the label follows the close.
			id := sg.PRNumber
			time.AfterFunc(b.rejectGrace, func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if sg, ok := b.suggestionFor(ctx, id); ok && sg.Status == store.StatusOpen {
					b.check(ctx, sg)
				}
			})
		}
	}
}

func (b *Bot) suggestionFor(ctx context.Context, number int) (store.Suggestion, bool) {
	sg, err := b.store.SuggestionByNumber(ctx, number)
	if errors.Is(err, store.ErrNotFound) {
		return sg, false // not opened by the bot
	}
	if err != nil {
		b.log.Error("find suggestion", "number", number, "err", err)
		return sg, false
	}
	return sg, true
}

// reconcile asks the host about every open suggestion, so merges, closes
// and accepted reports are noticed even if a webhook was lost.
func (b *Bot) reconcile(ctx context.Context) {
	if b.host == nil {
		return
	}
	open, err := b.store.OpenSuggestions(ctx)
	if err != nil {
		b.log.Error("list open suggestions", "err", err)
		return
	}
	for _, sg := range open {
		if ctx.Err() != nil {
			return
		}
		b.check(ctx, sg)
	}
}

// check resolves one open suggestion from its state on the host.
func (b *Bot) check(ctx context.Context, sg store.Suggestion) {
	isPR := sg.Kind != store.KindReport
	st, err := b.host.State(ctx, sg.PRNumber, isPR)
	if err != nil {
		b.log.Warn("read suggestion state", "number", sg.PRNumber, "err", err)
		return
	}
	switch {
	case !st.Closed:
	case isPR:
		b.resolve(ctx, sg, st.Merged)
	case st.HasLabel(b.cfg.AcceptedLabel):
		b.resolve(ctx, sg, true)
	case time.Since(st.ClosedAt) >= b.rejectGrace:
		b.resolve(ctx, sg, false)
	}
}

// resolve records the outcome of a suggestion and tells the user. A report
// that was rejected can still be accepted later, once.
func (b *Bot) resolve(ctx context.Context, sg store.Suggestion, accepted bool) {
	status := store.StatusClosed
	if accepted {
		status = store.StatusMerged
	}
	changed, err := b.store.ResolveSuggestion(ctx, sg.ID, status)
	if err == nil && !changed && accepted && sg.Kind == store.KindReport {
		changed, err = b.store.AcceptLate(ctx, sg.ID)
	}
	if err != nil {
		b.log.Error("resolve suggestion", "number", sg.PRNumber, "err", err)
		return
	}
	if !changed {
		return // repeated delivery
	}
	b.log.Info("suggestion resolved", "number", sg.PRNumber, "kind", sg.Kind, "status", status, "user", sg.TgID)

	var text string
	var kb *models.InlineKeyboardMarkup
	if accepted {
		// Show the new content right away, before the push webhook or the
		// next sync.
		if sg.Kind != store.KindReport {
			b.Sync(ctx)
		}
		u, err := b.store.User(ctx, sg.TgID)
		if err != nil {
			b.log.Error("load user", "err", err)
			return
		}
		text = fmt.Sprintf(map[string]string{
			store.KindEdit: b.t.AcceptedEdit, store.KindNew: b.t.AcceptedNew, store.KindReport: b.t.AcceptedReport,
		}[sg.Kind], sg.Title)
		text += " " + fmt.Sprintf(b.t.AcceptedCount, u.MergedCount)
		if rank := b.cfg.Ladder.For(u.MergedCount); rank != b.cfg.Ladder.For(u.MergedCount-1) {
			text += "\n\n" + fmt.Sprintf(b.t.RankUp, rank.Title)
		}
		if a := b.catalog().NoteByPath(sg.ArticlePath); a != nil {
			kb = keyboard(row(button(b.t.OpenTopic, cbOpen+a.ID)))
		}
	} else {
		text = fmt.Sprintf(map[string]string{
			store.KindEdit: b.t.RejectedEdit, store.KindNew: b.t.RejectedNew, store.KindReport: b.t.RejectedReport,
		}[sg.Kind], sg.Title)
		comment, err := b.host.LastReviewComment(ctx, sg.PRNumber)
		if err != nil {
			b.log.Warn("read review comments", "number", sg.PRNumber, "err", err)
		}
		if comment != "" {
			text += "\n\n" + fmt.Sprintf(b.t.ReviewComment, comment)
		}
	}
	// Users have a private chat with the bot, whose ID equals their user ID.
	b.send(ctx, sg.TgID, text, kb)
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

// syncLoop pulls periodically as a fallback for missed push webhooks, and
// checks open suggestions as a fallback for missed pull request and issue
// webhooks.
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
			b.reconcile(ctx)
		}
	}
}
