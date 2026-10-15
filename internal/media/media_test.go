package media

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/store"
)

type memCache map[string]string

func (m memCache) MediaFileID(_ context.Context, sha string) (string, error) {
	if id, ok := m[sha]; ok {
		return id, nil
	}
	return "", store.ErrNotFound
}
func (m memCache) PutMediaFileID(_ context.Context, sha, id string) error { m[sha] = id; return nil }
func (m memCache) DeleteMediaFileID(_ context.Context, sha string) error  { delete(m, sha); return nil }

type fakeSender struct{ calls int }

func (f *fakeSender) SendPhoto(_ context.Context, p *bot.SendPhotoParams) (*models.Message, error) {
	f.calls++
	io.Copy(io.Discard, p.Photo.(*models.InputFileUpload).Data)
	return &models.Message{Photo: []models.PhotoSize{
		{FileID: "small", Width: 90, Height: 90},
		{FileID: "big", Width: 800, Height: 600},
	}}, nil
}

func fixture(t *testing.T) *render.Result {
	t.Helper()
	dir := t.TempDir()
	var res render.Result
	for _, name := range []string{"a", "b"} {
		p := filepath.Join(dir, name+".png")
		os.WriteFile(p, []byte(name), 0o644)
		sha := name + "0000000000000000000000000000000"
		res.Media = append(res.Media, render.Media{ID: render.MediaID(sha), SHA256: sha, AbsPath: p})
	}
	res.Markdown = "text\n\n![](tg://photo?id=" + res.Media[0].ID + ")\n\n![](https://example.com/x.png)\n\n> ![](tg://photo?id=" + res.Media[1].ID + ")\n"
	return &res
}

func photo(id string) models.RichBlock {
	return models.RichBlock{Type: models.RichBlockTypePhoto, RichBlockPhoto: &models.RichBlockPhoto{
		Photo: []models.PhotoSize{{FileID: id + "-thumb", Width: 10, Height: 10}, {FileID: id, Width: 100, Height: 100}},
	}}
}

func TestAttachThenLearn(t *testing.T) {
	ctx := context.Background()
	res := fixture(t)
	cache := memCache{res.Media[1].SHA256: "cached-b"}
	r := &Resolver{Cache: cache, Log: slog.Default()}

	p, err := r.Prepare(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if got := p.Entries[0].Media.(*models.InputMediaPhoto).Media; got != "attach://"+res.Media[0].ID {
		t.Errorf("first entry = %q, want attach", got)
	}
	if got := p.Entries[1].Media.(*models.InputMediaPhoto).Media; got != "cached-b" {
		t.Errorf("second entry = %q, want cached file_id", got)
	}
	if !p.HasCached() {
		t.Error("HasCached = false")
	}

	msg := &models.Message{RichMessage: &models.RichMessage{Blocks: []models.RichBlock{
		{Type: models.RichBlockTypeParagraph, RichBlockParagraph: &models.RichBlockParagraph{}},
		photo("new-a"),
		photo("remote"),
		{Type: models.RichBlockTypeBlockQuotation, RichBlockBlockQuotation: &models.RichBlockBlockQuotation{
			Blocks: []models.RichBlock{photo("cached-b")},
		}},
	}}}
	r.Learn(ctx, p, res.Markdown, msg)
	if cache[res.Media[0].SHA256] != "new-a" {
		t.Errorf("learned file_id = %q, want new-a", cache[res.Media[0].SHA256])
	}
	if cache[res.Media[1].SHA256] != "cached-b" {
		t.Errorf("cached entry changed to %q", cache[res.Media[1].SHA256])
	}
}

func TestLearnSkipsOnMismatch(t *testing.T) {
	ctx := context.Background()
	res := fixture(t)
	cache := memCache{}
	r := &Resolver{Cache: cache, Log: slog.Default()}
	p, err := r.Prepare(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r.Learn(ctx, p, res.Markdown, &models.Message{RichMessage: &models.RichMessage{Blocks: []models.RichBlock{photo("x")}}})
	if len(cache) != 0 {
		t.Errorf("cache = %v, want empty", cache)
	}
}

func TestCacheChat(t *testing.T) {
	ctx := context.Background()
	res := fixture(t)
	cache := memCache{}
	s := &fakeSender{}
	r := &Resolver{Cache: cache, Sender: s, CacheChatID: -100, Log: slog.Default()}
	p, err := r.Prepare(ctx, res)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if s.calls != 2 {
		t.Errorf("uploads = %d, want 2", s.calls)
	}
	for _, e := range p.Entries {
		if got := e.Media.(*models.InputMediaPhoto).Media; got != "big" {
			t.Errorf("entry = %q, want largest size file_id", got)
		}
	}
	if cache[res.Media[0].SHA256] != "big" {
		t.Errorf("cache = %v", cache)
	}
}
