// Package media turns the local images of a converted article into
// rich message media entries, reusing Telegram file_ids where possible.
package media

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/store"
)

// Cache stores file_ids by file hash.
type Cache interface {
	MediaFileID(ctx context.Context, sha string) (string, error)
	PutMediaFileID(ctx context.Context, sha, fileID string) error
	DeleteMediaFileID(ctx context.Context, sha string) error
}

// Sender is the subset of the Telegram client used for uploads.
type Sender interface {
	SendPhoto(ctx context.Context, params *bot.SendPhotoParams) (*models.Message, error)
}

// Resolver prepares media entries.
type Resolver struct {
	Cache  Cache
	Sender Sender
	// CacheChatID, if set, is a chat where new images are uploaded once to
	// obtain a file_id before the article is sent. Without it, new images are
	// uploaded with the article itself and their file_ids are read back from
	// the sent message.
	CacheChatID int64
	Log         *slog.Logger
}

// Prepared is the media list for one message.
type Prepared struct {
	Entries []models.InputRichMessageMedia
	// pending are images attached in this message whose file_id is not known yet.
	pending map[string]render.Media
	files   []io.Closer
}

// Close releases the files opened for attachments.
func (p *Prepared) Close() {
	for _, f := range p.files {
		f.Close()
	}
	p.files = nil
}

// Prepare builds the media entries for res.
func (r *Resolver) Prepare(ctx context.Context, res *render.Result) (*Prepared, error) {
	p := &Prepared{pending: map[string]render.Media{}}
	for _, m := range res.Media {
		fileID, err := r.Cache.MediaFileID(ctx, m.SHA256)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			p.Close()
			return nil, err
		}
		if fileID != "" {
			r.Log.Debug("image from file_id cache", "file", m.AbsPath, "sha256", m.SHA256[:12])
		} else if r.CacheChatID != 0 {
			fileID, err = r.uploadToCacheChat(ctx, m)
			if err != nil {
				r.Log.Warn("upload to cache chat failed; attaching instead", "file", m.AbsPath, "err", err)
			} else {
				r.Log.Info("image uploaded to cache chat", "file", m.AbsPath, "sha256", m.SHA256[:12])
			}
		}
		if fileID != "" {
			p.Entries = append(p.Entries, models.InputRichMessageMedia{
				ID:    m.ID,
				Media: &models.InputMediaPhoto{Media: fileID},
			})
			continue
		}
		f, err := os.Open(m.AbsPath)
		if err != nil {
			p.Close()
			return nil, err
		}
		r.Log.Info("image attached to message", "file", m.AbsPath, "sha256", m.SHA256[:12])
		p.files = append(p.files, f)
		p.pending[m.ID] = m
		p.Entries = append(p.Entries, models.InputRichMessageMedia{
			ID:    m.ID,
			Media: &models.InputMediaPhoto{Media: "attach://" + m.ID, MediaAttachment: f},
		})
	}
	return p, nil
}

func (r *Resolver) uploadToCacheChat(ctx context.Context, m render.Media) (string, error) {
	f, err := os.Open(m.AbsPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	msg, err := r.Sender.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:              r.CacheChatID,
		Photo:               &models.InputFileUpload{Filename: filepath.Base(m.AbsPath), Data: f},
		Caption:             m.SHA256,
		DisableNotification: true,
	})
	if err != nil {
		return "", err
	}
	id := largest(msg.Photo)
	if id == "" {
		return "", errors.New("no photo in response")
	}
	if err := r.Cache.PutMediaFileID(ctx, m.SHA256, id); err != nil {
		return "", err
	}
	return id, nil
}

// Forget drops every cached file_id used by res, so that the next attempt
// uploads the files again. It is used when Telegram rejects a message.
func (r *Resolver) Forget(ctx context.Context, res *render.Result) {
	for _, m := range res.Media {
		if err := r.Cache.DeleteMediaFileID(ctx, m.SHA256); err != nil {
			r.Log.Warn("forget file_id", "err", err)
		}
	}
}

// HasCached reports whether any entry uses a cached file_id.
func (p *Prepared) HasCached() bool {
	return len(p.pending) < len(p.Entries)
}

var photoRef = regexp.MustCompile(`(?m)^[ \t>]*!\[[^\]]*\]\(\s*(tg://photo\?id=([A-Za-z0-9_-]+)|\S+)`)

// Learn reads the file_ids of newly attached images from the sent message.
// Photo blocks come back in document order, so they are matched with the
// image lines of the Markdown; if the counts disagree nothing is stored.
func (r *Resolver) Learn(ctx context.Context, p *Prepared, markdown string, msg *models.Message) {
	if len(p.pending) == 0 || msg == nil || msg.RichMessage == nil {
		return
	}
	var refs []string
	for _, m := range photoRef.FindAllStringSubmatch(markdown, -1) {
		refs = append(refs, m[2]) // empty for remote images
	}
	var ids []string
	collectPhotos(msg.RichMessage.Blocks, &ids)
	if len(ids) != len(refs) {
		r.Log.Warn("cannot match photos in sent message", "photos", len(ids), "images", len(refs))
		return
	}
	for i, ref := range refs {
		m, ok := p.pending[ref]
		if !ok || ids[i] == "" {
			continue
		}
		if err := r.Cache.PutMediaFileID(ctx, m.SHA256, ids[i]); err != nil {
			r.Log.Warn("store file_id", "err", err)
		} else {
			r.Log.Info("file_id learned from sent message", "file", m.AbsPath, "sha256", m.SHA256[:12])
		}
		delete(p.pending, ref)
	}
}

func collectPhotos(blocks []models.RichBlock, out *[]string) {
	for _, b := range blocks {
		switch {
		case b.RichBlockPhoto != nil:
			*out = append(*out, largest(b.RichBlockPhoto.Photo))
		case b.RichBlockList != nil:
			for _, it := range b.RichBlockList.Items {
				collectPhotos(it.Blocks, out)
			}
		case b.RichBlockBlockQuotation != nil:
			collectPhotos(b.RichBlockBlockQuotation.Blocks, out)
		case b.RichBlockDetails != nil:
			collectPhotos(b.RichBlockDetails.Blocks, out)
		case b.RichBlockCollage != nil:
			collectPhotos(b.RichBlockCollage.Blocks, out)
		case b.RichBlockSlideshow != nil:
			collectPhotos(b.RichBlockSlideshow.Blocks, out)
		}
	}
}

func largest(sizes []models.PhotoSize) string {
	best, id := -1, ""
	for _, s := range sizes {
		if s.Width*s.Height > best {
			best, id = s.Width*s.Height, s.FileID
		}
	}
	return id
}
