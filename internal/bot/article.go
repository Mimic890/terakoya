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
	res := render.Convert(b.catalog(), a, render.Options{})
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
		b.log.Debug("rejected markdown", "article", a.Path, "markdown", res.Markdown)
		b.send(ctx, chatID, b.t.SendFailed, nil)
		return false
	}
	b.log.Info("article sent", "id", a.ID, "chat", chatID, "images", len(res.Media))
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
			b.msgs.seen(ctx, chatID, msg.ID)
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
	msg, err := b.api.SendDocument(ctx, &tg.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: path.Base(a.Path), Data: bytes.NewReader(a.Raw)},
	})
	if err != nil {
		b.log.Error("send document", "article", a.Path, "err", err)
		return
	}
	b.msgs.seen(ctx, chatID, msg.ID)
}
