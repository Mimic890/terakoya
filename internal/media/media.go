// Package media turns the local images of a converted note into rich
// message media entries, reusing Telegram file_ids where possible. SVG
// drawings are rasterized to PNG once; the file_id of the PNG is cached by
// the hash of the SVG source.
package media

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/raster"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/store"
)

// Cache stores file_ids by cache key: the file hash, prefixed for SVGs
// with the rasterizer settings.
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
	Raster      raster.Rasterizer
	Log         *slog.Logger
}

// Key is the cache key of an image: its SHA-256, and for an SVG the
// rasterizer tag in front, so an unchanged drawing is never converted or
// uploaded again while a changed one gets a new key.
func (r *Resolver) Key(m render.Media) string {
	if m.SVG {
		tag := "svg"
		if r.Raster != nil {
			tag = r.Raster.Tag()
		}
		return tag + ":" + m.SHA256
	}
	return m.SHA256
}

// Prepared is the media list for one message.
type Prepared struct {
	Entries []models.InputRichMessageMedia
	// Failed are the media IDs of SVGs that could not be rasterized; the
	// message is sent without them (see Strip).
	Failed map[string]error
	// pending are images attached in this message whose file_id is not
	// known yet, by media ID, with their cache keys.
	pending map[string]pendingMedia
}

type pendingMedia struct {
	render.Media
	key string
}

var imageRef = regexp.MustCompile(`(?m)^[ \t>]*!\[[^\]]*\]\(tg://photo\?id=([A-Za-z0-9_-]+)[^)]*\)[ \t]*\n?`)

// Strip removes the images that failed to convert from the Markdown.
func (p *Prepared) Strip(markdown string) string {
	if len(p.Failed) == 0 {
		return markdown
	}
	out := imageRef.ReplaceAllStringFunc(markdown, func(line string) string {
		if p.Failed[imageRef.FindStringSubmatch(line)[1]] != nil {
			return ""
		}
		return line
	})
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return out
}

// Prepare builds the media entries for one part of a note. An SVG is
// rasterized only when its PNG is not cached; if that fails, the image is
// left out and listed in Failed.
func (r *Resolver) Prepare(ctx context.Context, part render.Part) (*Prepared, error) {
	p := &Prepared{pending: map[string]pendingMedia{}, Failed: map[string]error{}}
	for _, m := range part.Media {
		key := r.Key(m)
		fileID, err := r.Cache.MediaFileID(ctx, key)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if fileID != "" {
			r.Log.Debug("image from file_id cache", "file", m.Path, "key", short(key))
			p.Entries = append(p.Entries, models.InputRichMessageMedia{ID: m.ID, Media: &models.InputMediaPhoto{Media: fileID}})
			continue
		}
		data, name, err := r.load(ctx, m)
		if err != nil {
			r.Log.Error("image left out of the message", "file", m.Path, "err", err)
			p.Failed[m.ID] = err
			continue
		}
		if r.CacheChatID != 0 {
			fileID, err = r.uploadToCacheChat(ctx, key, name, data)
			if err != nil {
				r.Log.Warn("upload to cache chat failed; attaching instead", "file", m.Path, "err", err)
			} else {
				r.Log.Info("image uploaded to cache chat", "file", m.Path, "key", short(key))
				p.Entries = append(p.Entries, models.InputRichMessageMedia{ID: m.ID, Media: &models.InputMediaPhoto{Media: fileID}})
				continue
			}
		}
		r.Log.Info("image attached to message", "file", m.Path, "key", short(key))
		p.pending[m.ID] = pendingMedia{m, key}
		p.Entries = append(p.Entries, models.InputRichMessageMedia{
			ID:    m.ID,
			Media: &models.InputMediaPhoto{Media: "attach://" + m.ID, MediaAttachment: bytes.NewReader(data)},
		})
	}
	return p, nil
}

// load reads an image, rasterizing SVGs, and returns its bytes and a file
// name for the upload.
func (r *Resolver) load(ctx context.Context, m render.Media) ([]byte, string, error) {
	name := filepath.Base(m.AbsPath)
	if !m.SVG {
		data, err := os.ReadFile(m.AbsPath)
		return data, name, err
	}
	if r.Raster == nil {
		return nil, "", errors.New("no SVG rasterizer configured")
	}
	data, err := r.Raster.Rasterize(ctx, m.AbsPath)
	if err != nil {
		return nil, "", err
	}
	r.Log.Info("SVG rasterized", "file", m.Path, "bytes", len(data))
	return data, strings.TrimSuffix(name, filepath.Ext(name)) + ".png", nil
}

func short(key string) string {
	if i := strings.LastIndexByte(key, ':'); i >= 0 && len(key)-i > 13 {
		return key[:i+13]
	}
	if len(key) > 12 {
		return key[:12]
	}
	return key
}

func (r *Resolver) uploadToCacheChat(ctx context.Context, key, name string, data []byte) (string, error) {
	msg, err := r.Sender.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID:              r.CacheChatID,
		Photo:               &models.InputFileUpload{Filename: name, Data: bytes.NewReader(data)},
		Caption:             key,
		DisableNotification: true,
	})
	if err != nil {
		return "", err
	}
	id := largest(msg.Photo)
	if id == "" {
		return "", errors.New("no photo in response")
	}
	if err := r.Cache.PutMediaFileID(ctx, key, id); err != nil {
		return "", err
	}
	return id, nil
}

// Forget drops every cached file_id used by a part, so that the next
// attempt uploads the files again. It is used when Telegram rejects a
// message.
func (r *Resolver) Forget(ctx context.Context, part render.Part) {
	for _, m := range part.Media {
		if err := r.Cache.DeleteMediaFileID(ctx, r.Key(m)); err != nil {
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
		if err := r.Cache.PutMediaFileID(ctx, m.key, ids[i]); err != nil {
			r.Log.Warn("store file_id", "err", err)
		} else {
			r.Log.Info("file_id learned from sent message", "file", m.Path, "key", short(m.key))
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
