package suggest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/Mimic890/terakoya/internal/render"
)

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
}

// Slug turns a title into a lowercase ASCII slug of at most max bytes,
// transliterating Cyrillic.
func Slug(s string, max int) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		var part string
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			part = string(r)
		default:
			if t, ok := translit[r]; ok {
				part = t
			}
		}
		if part == "" {
			if t, ok := translit[r]; ok && t == "" {
				continue // soft and hard signs vanish without a separator
			}
			dash = b.Len() > 0
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(part)
	}
	out := b.String()
	if len(out) > max {
		out = strings.TrimRight(out[:max], "-")
	}
	return out
}

// ShortID returns a random 6-character hex ID.
func ShortID() string {
	var b [3]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Branch names the branch of a suggestion: suggest/<tg_id>-<slug>-<short_id>,
// where the slug comes from the article's file name without its order prefix.
func Branch(tgID int64, articlePath, shortID string) string {
	name := strings.TrimSuffix(path.Base(articlePath), ".md")
	slug := Slug(orderPrefix.ReplaceAllString(name, ""), 40)
	if slug == "" {
		slug = "topic"
	}
	return fmt.Sprintf("suggest/%d-%s-%s", tgID, slug, shortID)
}

// ImageName makes a safe file name for an uploaded image, unique among
// taken. It returns "" if the extension is not a supported image format.
func ImageName(original string, taken []string) string {
	ext := strings.ToLower(path.Ext(original))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if !render.ImageExts[ext] {
		return ""
	}
	base := Slug(strings.TrimSuffix(path.Base(original), path.Ext(original)), 40)
	if base == "" {
		base = "image"
	}
	name := base + ext
	for i := 2; contains(taken, name); i++ {
		name = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	return name
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
