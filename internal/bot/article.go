package bot

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/store"
)

// sendArticle sends a note as one or more rich messages, with the buttons
// under the last one, and reports whether it succeeded. The messages are
// recorded as a group, so moving on removes all of them. fromPins is set
// when it was opened from the pinned list, which «Назад» then returns to.
func (b *Bot) sendArticle(ctx context.Context, chatID int64, n *content.Note, fromPins bool) bool {
	res := render.Convert(b.catalog(), n, render.Options{Continued: b.t.Continued})
	// In a private chat the chat ID is the user's.
	pinned, err := b.store.Pinned(ctx, chatID, n.ID)
	if err != nil {
		b.log.Error("pinned", "err", err)
	}
	var sent []int
	defer func() {
		if err := b.store.SetMessageGroup(ctx, chatID, sent); err != nil {
			b.log.Error("store message group", "err", err)
		}
	}()
	for i, part := range res.Parts {
		var kb *models.InlineKeyboardMarkup
		if i == len(res.Parts)-1 {
			kb = articleKeyboard(b.t, n, pinned, fromPins, b.emoji.Pin())
		}
		msg, err := b.sendRich(ctx, chatID, n, part, kb)
		if err != nil {
			b.log.Error("send note", "note", n.Path, "part", i+1, "of", len(res.Parts), "err", err)
			b.log.Debug("rejected markdown", "note", n.Path, "markdown", part.Markdown)
			if m := b.send(ctx, chatID, b.t.SendFailed, keyboard(
				row(button(b.t.DownloadMD, cbDownload+n.ID)),
				row(button(b.t.Back, cbBack+n.ID)),
			)); m != nil && len(sent) > 0 {
				sent = append(sent, m.ID)
			}
			return false
		}
		sent = append(sent, msg.ID)
	}
	b.log.Info("note sent", "id", n.ID, "path", n.Path, "chat", chatID, "parts", len(res.Parts))
	return true
}

func (b *Bot) sendRich(ctx context.Context, chatID int64, n *content.Note, part render.Part, kb *models.InlineKeyboardMarkup) (*models.Message, error) {
	for attempt := 0; ; attempt++ {
		prep, err := b.media.Prepare(ctx, part)
		if err != nil {
			return nil, err
		}
		md := prep.Strip(part.Markdown)
		p := &tg.SendRichMessageParams{
			ChatID:      chatID,
			RichMessage: models.InputRichMessage{Markdown: md, Media: prep.Entries},
		}
		if kb != nil {
			p.ReplyMarkup = kb
		}
		msg, err := b.api.SendRichMessage(ctx, p)
		if err == nil {
			b.msgs.seen(ctx, chatID, msg.ID)
			b.media.Learn(ctx, prep, md, msg)
			return msg, nil
		}
		if plain, ok := b.plainInline(kb); ok {
			// Once more without custom emoji icons.
			p.ReplyMarkup = plain
			if msg, err2 := b.api.SendRichMessage(ctx, p); err2 == nil {
				b.emoji.rejected(err)
				b.msgs.seen(ctx, chatID, msg.ID)
				b.media.Learn(ctx, prep, md, msg)
				return msg, nil
			}
		}
		// A cached file_id can become invalid; upload the files again once.
		if attempt == 0 && prep.HasCached() {
			b.log.Warn("send with cached media failed; retrying with uploads", "note", n.Path, "err", err)
			b.media.Forget(ctx, part)
			continue
		}
		return nil, err
	}
}

func (b *Bot) sendMarkdownFile(ctx context.Context, chatID int64, n *content.Note) {
	msg, err := b.api.SendDocument(ctx, &tg.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: n.FileName(), Data: bytes.NewReader(n.Raw)},
	})
	if err != nil {
		b.log.Error("send document", "note", n.Path, "err", err)
		return
	}
	b.msgs.seen(ctx, chatID, msg.ID)
}

// sendArchive sends the whole vault as a zip, built once per commit; later
// requests reuse the file_id.
func (b *Bot) sendArchive(ctx context.Context, chatID int64) {
	b.mu.RLock()
	sha := b.sha
	b.mu.RUnlock()
	name := fmt.Sprintf(b.t.ArchiveName, time.Now().Format("2006-01-02"))
	if sha != "" {
		if id, err := b.store.ArchiveFileID(ctx, sha); err == nil {
			if b.sendDocument(ctx, chatID, &models.InputFileString{Data: id}) {
				return
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			b.log.Error("archive cache", "err", err)
		}
	}
	data, err := zipVault(b.catalog().Root)
	if err != nil {
		b.log.Error("build archive", "err", err)
		b.send(ctx, chatID, b.t.SendFailed, nil)
		return
	}
	msg, err := b.api.SendDocument(ctx, &tg.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: name, Data: bytes.NewReader(data)},
	})
	if err != nil {
		b.log.Error("send archive", "err", err)
		b.send(ctx, chatID, b.t.SendFailed, nil)
		return
	}
	b.msgs.seen(ctx, chatID, msg.ID)
	if sha != "" && msg.Document != nil {
		if err := b.store.PutArchiveFileID(ctx, sha, msg.Document.FileID); err != nil {
			b.log.Error("store archive", "err", err)
		}
	}
	b.log.Info("vault archive sent", "commit", sha, "bytes", len(data))
}

func (b *Bot) sendDocument(ctx context.Context, chatID int64, doc models.InputFile) bool {
	msg, err := b.api.SendDocument(ctx, &tg.SendDocumentParams{ChatID: chatID, Document: doc})
	if err != nil {
		b.log.Warn("send cached archive", "err", err)
		return false
	}
	b.msgs.seen(ctx, chatID, msg.ID)
	return true
}

// zipVault packs every file of the vault except hidden ones (.git,
// .obsidian, .trash, .gitignore).
func zipVault(root string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if content.Hidden(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		w, err := zw.CreateHeader(&zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate, Modified: time.Now(),
			Flags: 0x800}) // UTF-8 names
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// retire removes a note or menu that has been replaced by a newer one: all
// messages of its group. Telegram refuses to delete messages older than 48
// hours; the last part then loses its keyboard, so its buttons cannot be
// used any more, and other parts just stay.
func (b *Bot) retire(ctx context.Context, msg *models.Message) {
	chatID := msg.Chat.ID
	ids, err := b.store.MessageGroup(ctx, chatID, msg.ID)
	if err != nil {
		b.log.Error("message group", "err", err)
		ids = []int{msg.ID}
	}
	if err := b.store.DropMessageGroup(ctx, chatID, msg.ID); err != nil {
		b.log.Error("drop message group", "err", err)
	}
	for i, id := range ids {
		_, err := b.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: chatID, MessageID: id})
		if err == nil {
			b.msgs.gone(ctx, chatID, id)
			continue
		}
		if i < len(ids)-1 {
			b.log.Debug("delete message failed; it stays", "msg", id, "err", err)
			continue
		}
		b.log.Debug("delete message failed; removing its keyboard", "err", err)
		b.msgs.dropMenu(ctx, chatID, id)
		_, err = b.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
			ChatID: chatID, MessageID: id, ReplyMarkup: keyboard(),
		})
		if err != nil {
			b.log.Debug("clear keyboard", "err", err)
		}
	}
}
