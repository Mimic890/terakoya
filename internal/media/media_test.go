package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

type fakeSender struct {
	calls int
	last  *bot.SendPhotoParams
}

func (f *fakeSender) SendPhoto(_ context.Context, p *bot.SendPhotoParams) (*models.Message, error) {
	f.calls++
	f.last = p
	io.Copy(io.Discard, p.Photo.(*models.InputFileUpload).Data)
	return &models.Message{Photo: []models.PhotoSize{
		{FileID: "small", Width: 90, Height: 90},
		{FileID: "big", Width: 800, Height: 600},
	}}, nil
}

func fixture(t *testing.T) *render.Part {
	t.Helper()
	dir := t.TempDir()
	var res render.Part
	for _, name := range []string{"a", "b"} {
		p := filepath.Join(dir, name+".png")
		os.WriteFile(p, []byte(name), 0o644)
		sha := name + "0000000000000000000000000000000"
		res.Media = append(res.Media, render.Media{ID: render.MediaID(sha), SHA256: sha, AbsPath: p, Path: name + ".png"})
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

	p, err := r.Prepare(ctx, *res)
	if err != nil {
		t.Fatal(err)
	}
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
	p, err := r.Prepare(ctx, *res)
	if err != nil {
		t.Fatal(err)
	}
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
	p, err := r.Prepare(ctx, *res)
	if err != nil {
		t.Fatal(err)
	}
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

// fakeRaster counts conversions and fails for sources containing "broken".
type fakeRaster struct{ calls int }

func (f *fakeRaster) Tag() string { return "fake-2x" }
func (f *fakeRaster) Rasterize(_ context.Context, path string) ([]byte, error) {
	f.calls++
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "broken") {
		return nil, errors.New("bad svg")
	}
	return append([]byte("\x89PNG:"), data...), nil
}

func svgPart(t *testing.T, dir, name, src string) render.Part {
	t.Helper()
	p := filepath.Join(dir, name)
	os.WriteFile(p, []byte(src), 0o644)
	sum := sha256.Sum256([]byte(src))
	sha := hex.EncodeToString(sum[:])
	m := render.Media{ID: render.MediaID(sha), SHA256: sha, AbsPath: p, Path: name, SVG: true}
	return render.Part{Markdown: "a\n\n![](tg://photo?id=" + m.ID + ")\n\nb\n", Media: []render.Media{m}}
}

func TestSVGConvertedOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cache := memCache{}
	s := &fakeSender{}
	ras := &fakeRaster{}
	r := &Resolver{Cache: cache, Sender: s, CacheChatID: -100, Raster: ras, Log: slog.Default()}

	part := svgPart(t, dir, "рис.svg", "<svg>v1</svg>")
	p, err := r.Prepare(ctx, part)
	if err != nil || ras.calls != 1 || s.calls != 1 || len(p.Failed) != 0 {
		t.Fatalf("first send: %v, conversions %d uploads %d", err, ras.calls, s.calls)
	}
	key := "fake-2x:" + part.Media[0].SHA256
	if cache[key] != "big" {
		t.Errorf("cache = %v, want the file_id under %s", cache, key)
	}
	if s.last.Photo.(*models.InputFileUpload).Filename != "рис.png" {
		t.Errorf("upload name = %q", s.last.Photo.(*models.InputFileUpload).Filename)
	}

	// Second send: no conversion, no upload.
	p, _ = r.Prepare(ctx, part)
	if ras.calls != 1 || s.calls != 1 || p.Entries[0].Media.(*models.InputMediaPhoto).Media != "big" {
		t.Errorf("second send converted or uploaded: conversions %d uploads %d", ras.calls, s.calls)
	}

	// A changed drawing has a new hash and is converted once more.
	changed := svgPart(t, dir, "рис.svg", "<svg>v2</svg>")
	r.Prepare(ctx, changed)
	if ras.calls != 2 || s.calls != 2 {
		t.Errorf("changed SVG: conversions %d uploads %d", ras.calls, s.calls)
	}
}

func TestSVGFailureLeavesImageOut(t *testing.T) {
	ctx := context.Background()
	part := svgPart(t, t.TempDir(), "плохой.svg", "<svg>broken")
	r := &Resolver{Cache: memCache{}, Raster: &fakeRaster{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p, err := r.Prepare(ctx, part)
	if err != nil || len(p.Entries) != 0 || p.Failed[part.Media[0].ID] == nil {
		t.Fatalf("prepare = %+v, %v", p, err)
	}
	if got := p.Strip(part.Markdown); got != "a\n\nb\n" {
		t.Errorf("stripped = %q", got)
	}
}
