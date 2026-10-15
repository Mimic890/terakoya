package bot

import (
	"bytes"
	"context"
	"path"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/render"
)

// sendArticle sends a as a rich message and reports whether it succeeded.
func (b *Bot) sendArticle(ctx context.Context, chatID int64, a *content.Article) bool {
	res := render.Convert(b.catalog(), a, render.Options{BotUsername: b.cfg.BotUsername})
	if errs := res.LimitErrors(); len(errs) > 0 {
		b.log.Warn("article exceeds message limits", "article", a.Path, "issues", errs)
		b.send(ctx, chatID, b.t.TooLarge, keyboard(
			row(button(b.t.DownloadMD, cbDownload+a.ID)),
			row(button(b.t.Back, cbBack+a.ID)),
		))
		return false
	}

	if err := b.sendRich(ctx, chatID, a, res); err != nil {
		b.log.Error("send article", "article", a.Path, "err", err)
		b.send(ctx, chatID, b.t.SendFailed, nil)
		return false
	}
	return true
}

func (b *Bot) sendRich(ctx context.Context, chatID int64, a *content.Article, res *render.Result) error {
	for attempt := 0; ; attempt++ {
		prep, err := b.media.Prepare(ctx, res)
		if err != nil {
			return err
		}
		msg, err := b.api.SendRichMessage(ctx, &tg.SendRichMessageParams{
			ChatID: chatID,
			RichMessage: models.InputRichMessage{
				Markdown: res.Markdown,
				Media:    prep.Entries,
			},
			ReplyMarkup: articleKeyboard(b.t, a),
		})
		if err == nil {
			b.media.Learn(ctx, prep, res.Markdown, msg)
			prep.Close()
			return nil
		}
		prep.Close()
		// A cached file_id can become invalid; upload the files again once.
		if attempt == 0 && prep.HasCached() {
			b.log.Warn("send with cached media failed; retrying with uploads", "article", a.Path, "err", err)
			b.media.Forget(ctx, res)
			continue
		}
		return err
	}
}

func (b *Bot) sendMarkdownFile(ctx context.Context, chatID int64, a *content.Article) {
	_, err := b.api.SendDocument(ctx, &tg.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: path.Base(a.Path), Data: bytes.NewReader(a.Raw)},
	})
	if err != nil {
		b.log.Error("send document", "article", a.Path, "err", err)
	}
}

func (b *Bot) deleteMessage(ctx context.Context, msg *models.Message) {
	if _, err := b.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID}); err != nil {
		b.log.Debug("delete message", "err", err)
	}
}

func (b *Bot) clearKeyboard(ctx context.Context, msg *models.Message) {
	_, err := b.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, ReplyMarkup: keyboard(),
	})
	if err != nil {
		b.log.Debug("clear keyboard", "err", err)
	}
}
