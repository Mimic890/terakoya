package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/store"
)

// /broadcast: an admin sends a message, sees it as users will, and sends
// it to everyone who has started the bot, except admins and users who
// blocked it. Sending runs in the background at a rate Telegram allows,
// with a progress message for the admin.

const (
	stateBroadcastWait    = "broadcast_wait"    // waiting for the message
	stateBroadcastConfirm = "broadcast_confirm" // preview shown, «Отправить всем?»

	cbBroadcastYes  = "bc:y"
	cbBroadcastEdit = "bc:e"
	cbBroadcast     = "bc:"
)

const (
	// broadcastGap keeps sending under 25 messages a second.
	broadcastGap = 40 * time.Millisecond
	// progressEvery is how often the admin's progress message is updated.
	progressEvery = 10 * time.Second
	// maxRetries is how often a message is sent again after a 429.
	maxRetries = 5
)

// startBroadcast handles /broadcast.
func (b *Bot) startBroadcast(ctx context.Context, chatID, userID int64) {
	unlock := b.userLocks.lock(userID)
	defer unlock()
	// Like any new dialog, this one replaces what was going on.
	if d, _ := b.loadDialog(ctx, userID, chatID); d != nil {
		b.dropButtons(ctx, d)
	}
	d := &dialog{chatID: chatID, ses: store.Session{TgID: userID, State: stateBroadcastWait}}
	b.prompt(ctx, d, b.t.BroadcastAsk, keyboard(cancelRow(b.t.Cancel)))
}

// onBroadcastMessage takes the message to send, and shows its preview.
func (b *Bot) onBroadcastMessage(ctx context.Context, d *dialog, m *models.Message) {
	text, ents := m.Text, m.Entities
	photo := ""
	if len(m.Photo) > 0 {
		text, ents = m.Caption, m.CaptionEntities
		photo = m.Photo[len(m.Photo)-1].FileID
	}
	switch {
	case m.MediaGroupID != "" && m.MediaGroupID == d.p.MediaGroup:
		// The other pictures of an album.
		b.note(ctx, d, b.t.BroadcastOnePhoto)
		return
	case m.Document != nil, m.Video != nil, m.Sticker != nil, m.Voice != nil, m.Audio != nil, m.Animation != nil, m.VideoNote != nil,
		strings.TrimSpace(text) == "" && photo == "":
		b.note(ctx, d, b.t.BroadcastOnlyText)
		return
	}
	// A new message replaces the previous preview.
	b.dropPreview(ctx, d)
	d.p.Markdown, d.p.Photo, d.p.MediaGroup = entitiesMarkdown(text, ents), photo, m.MediaGroupID
	b.dropButtons(ctx, d)
	msg, err := b.sendBroadcast(ctx, d.chatID, d.p.Markdown, d.p.Photo)
	if err != nil {
		b.log.Warn("broadcast preview", "err", err)
		d.ses.State = stateBroadcastWait
		b.prompt(ctx, d, b.t.BroadcastPreviewFailed, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	d.track(msg)
	d.p.Preview = msg.ID
	ids, err := b.recipients(ctx)
	if err != nil {
		b.log.Error("recipients", "err", err)
	}
	d.ses.State = stateBroadcastConfirm
	b.prompt(ctx, d, fmt.Sprintf(b.t.BroadcastConfirm, len(ids)), keyboard(
		row(button(b.t.BroadcastYes, cbBroadcastYes), button(b.t.BroadcastEdit, cbBroadcastEdit), button(b.t.No, cbCancel))))
}

// dropPreview deletes the preview and the question under it.
func (b *Bot) dropPreview(ctx context.Context, d *dialog) {
	if d.p.Preview == 0 {
		return
	}
	gone := []int{d.p.Preview}
	if d.p.PromptID != 0 {
		gone = append(gone, d.p.PromptID)
		d.p.PromptID = 0
	}
	b.dropMessages(ctx, d.chatID, gone)
	d.p.Msgs = without(d.p.Msgs, gone)
	d.p.Preview = 0
}

func without(ids, drop []int) []int {
	var out []int
	for _, id := range ids {
		if !containsInt(drop, id) {
			out = append(out, id)
		}
	}
	return out
}

func containsInt(ids []int, id int) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// onBroadcastCallback handles «Да, отправить» and «Изменить».
func (b *Bot) onBroadcastCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	if !b.cfg.IsAdmin(q.From.ID) {
		b.answer(ctx, q, "", false)
		return
	}
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	d, _ := b.loadDialog(ctx, q.From.ID, msg.Chat.ID)
	if d == nil || d.ses.State != stateBroadcastConfirm || d.p.PromptID != msg.ID {
		b.answer(ctx, q, b.t.Expired, true)
		b.dropButtonsOf(ctx, msg)
		return
	}
	b.answer(ctx, q, "", false)
	if q.Data == cbBroadcastEdit {
		b.dropPreview(ctx, d)
		d.ses.State = stateBroadcastWait
		b.prompt(ctx, d, b.t.BroadcastAsk, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	// One broadcast at a time: two would together go over Telegram's
	// rate limit. The dialog stays, so the admin can press again later.
	if !b.broadcasting.TryLock() {
		b.send(ctx, d.chatID, b.t.BroadcastBusy, nil)
		return
	}
	b.endDialog(ctx, d)
	// The preview stays as the record of what went out.
	b.dropMessages(ctx, d.chatID, without(d.p.Msgs, []int{d.p.Preview}))
	bc := &store.Broadcast{AdminID: q.From.ID, Markdown: d.p.Markdown, Photo: d.p.Photo}
	if _, err := b.launchBroadcast(ctx, bc, d.chatID); err != nil {
		b.log.Error("start broadcast", "err", err)
		b.send(ctx, d.chatID, b.t.SubmitFailed, nil)
	}
}

// launchBroadcast stores a broadcast with its recipients and sends it in
// the background, with a progress message in chatID unless it is 0. The
// caller holds b.broadcasting, which is released when sending ends or
// when this fails.
func (b *Bot) launchBroadcast(ctx context.Context, bc *store.Broadcast, chatID int64) (int, error) {
	ids, err := b.recipients(ctx)
	if err == nil {
		err = b.store.CreateBroadcast(ctx, bc, ids)
	}
	if err != nil {
		b.broadcasting.Unlock()
		return 0, err
	}
	if chatID != 0 {
		if m := b.send(ctx, chatID, fmt.Sprintf(b.t.BroadcastProgress, 0, len(ids)), nil); m != nil {
			if err := b.store.SetBroadcastProgress(ctx, bc, chatID, m.ID); err != nil {
				b.log.Error("store broadcast progress", "err", err)
			}
		}
	}
	b.log.Info("broadcast started", "id", bc.ID, "admin", bc.AdminID, "recipients", len(ids))
	b.later(ctx, func(ctx context.Context) {
		defer b.broadcasting.Unlock()
		b.runBroadcast(ctx, bc)
	})
	return len(ids), nil
}

// resumeBroadcasts goes on with broadcasts a restart cut short.
func (b *Bot) resumeBroadcasts(ctx context.Context) {
	open, err := b.store.UnfinishedBroadcasts(ctx)
	if err != nil {
		b.log.Error("unfinished broadcasts", "err", err)
		return
	}
	for _, bc := range open {
		if b.stopping() {
			return
		}
		b.broadcasting.Lock()
		if err := b.store.ResumeBroadcast(ctx, &bc); err != nil {
			b.broadcasting.Unlock()
			b.log.Error("resume broadcast", "id", bc.ID, "err", err)
			continue
		}
		b.log.Info("broadcast resumed", "id", bc.ID, "done", bc.Sent+bc.Blocked+bc.Failed, "recipients", bc.Recipients)
		b.runBroadcast(ctx, &bc)
		b.broadcasting.Unlock()
	}
}

// recipients are the users a broadcast goes to.
func (b *Bot) recipients(ctx context.Context) ([]int64, error) {
	return b.store.Recipients(ctx, b.cfg.Admins)
}

// sendBroadcast sends a broadcast message: the picture first, then the
// text, as one rich message.
func (b *Bot) sendBroadcast(ctx context.Context, chatID int64, md, photo string) (*models.Message, error) {
	p := &tg.SendRichMessageParams{ChatID: chatID, RichMessage: models.InputRichMessage{Markdown: md}}
	if photo != "" {
		p.RichMessage.Markdown = strings.TrimSpace("![](tg://photo?id=p)\n\n" + md)
		p.RichMessage.Media = []models.InputRichMessageMedia{{ID: "p", Media: &models.InputMediaPhoto{Media: photo}}}
	}
	msg, err := b.api.SendRichMessage(ctx, p)
	if err != nil {
		return nil, err
	}
	b.msgs.seen(ctx, chatID, msg.ID)
	return msg, nil
}

// runBroadcast sends to every recipient still pending, in turn, and keeps
// the admin's progress message and the stored counts up to date. When the
// bot shuts down it stops between two recipients; the broadcast stays
// unfinished and resumes on the next start.
func (b *Bot) runBroadcast(ctx context.Context, bc *store.Broadcast) {
	ids, err := b.store.PendingRecipients(ctx, bc.ID)
	if err != nil {
		b.log.Error("broadcast recipients", "id", bc.ID, "err", err)
		return
	}
	last := time.Now()
	for i, id := range ids {
		if i > 0 {
			b.sleep(b.stopCtx, b.broadcastGap)
		}
		if b.stopping() {
			if err := b.store.UpdateBroadcast(ctx, bc, false); err != nil {
				b.log.Error("store broadcast", "err", err)
			}
			b.log.Info("broadcast paused for shutdown; it resumes on start", "id", bc.ID, "left", len(ids)-i)
			return
		}
		b.deliver(ctx, bc, id)
		if time.Since(last) >= b.progressEvery {
			last = time.Now()
			b.progress(ctx, bc, fmt.Sprintf(b.t.BroadcastProgress, bc.Sent+bc.Blocked+bc.Failed, bc.Recipients))
			if err := b.store.UpdateBroadcast(ctx, bc, false); err != nil {
				b.log.Error("store broadcast", "err", err)
			}
		}
	}
	if err := b.store.UpdateBroadcast(ctx, bc, true); err != nil {
		b.log.Error("store broadcast", "err", err)
	}
	b.log.Info("broadcast finished", "id", bc.ID, "sent", bc.Sent, "blocked", bc.Blocked, "failed", bc.Failed)
	b.progress(ctx, bc, fmt.Sprintf(b.t.BroadcastDone, bc.Sent, bc.Blocked, bc.Failed))
}

// deliver sends the broadcast to one user. After a 429 it waits as long
// as Telegram asks and tries again; a user who blocked the bot is marked
// and skipped from then on. The recipient is marked sending first, so a
// crash cannot lead to a second copy.
func (b *Bot) deliver(ctx context.Context, bc *store.Broadcast, userID int64) {
	if err := b.store.MarkRecipient(ctx, bc.ID, userID, store.RecipientSending); err != nil {
		b.log.Error("mark broadcast recipient", "err", err)
		bc.Failed++
		return
	}
	outcome := b.deliverOnce(ctx, bc, userID)
	switch outcome {
	case store.RecipientSent:
		bc.Sent++
	case store.RecipientBlocked:
		bc.Blocked++
	default:
		bc.Failed++
	}
	if err := b.store.MarkRecipient(ctx, bc.ID, userID, outcome); err != nil {
		b.log.Error("mark broadcast recipient", "err", err)
	}
}

func (b *Bot) deliverOnce(ctx context.Context, bc *store.Broadcast, userID int64) string {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err := b.sendBroadcast(ctx, userID, bc.Markdown, bc.Photo)
		var wait *tg.TooManyRequestsError
		switch {
		case err == nil:
			return store.RecipientSent
		case errors.As(err, &wait):
			b.log.Warn("broadcast: too many requests", "retry_after", wait.RetryAfter)
			b.sleep(b.stopCtx, time.Duration(max(wait.RetryAfter, 1))*time.Second)
			continue
		case errors.Is(err, tg.ErrorForbidden):
			if err := b.store.MarkBlocked(ctx, userID); err != nil {
				b.log.Error("mark blocked", "err", err)
			}
			return store.RecipientBlocked
		default:
			b.log.Warn("broadcast: not delivered", "user", userID, "err", err)
			return store.RecipientFailed
		}
	}
	b.log.Warn("broadcast: still too many requests, gave up", "user", userID)
	return store.RecipientFailed
}

// progress shows the state of a broadcast on its progress message, or in
// a new one.
func (b *Bot) progress(ctx context.Context, bc *store.Broadcast, text string) {
	if bc.ProgressChat == 0 {
		return // from the admin socket, which has no chat
	}
	if bc.ProgressMsg != 0 {
		_, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{ChatID: bc.ProgressChat, MessageID: bc.ProgressMsg, Text: text})
		if err == nil || strings.Contains(err.Error(), "message is not modified") {
			return
		}
		b.log.Debug("edit broadcast progress", "err", err)
	}
	if m := b.send(ctx, bc.ProgressChat, text, nil); m != nil {
		if err := b.store.SetBroadcastProgress(ctx, bc, bc.ProgressChat, m.ID); err != nil {
			b.log.Error("store broadcast progress", "err", err)
		}
	}
}

// sleep waits d, or until ctx ends.
func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
