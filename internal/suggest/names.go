package suggest

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
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
// where the slug comes from the note's file name without its order prefix
// and trailing "(…)".
func Branch(tgID int64, articlePath, shortID string) string {
	slug := Slug(content.TitleFromFile(path.Base(articlePath)), 40)
	if slug == "" {
		slug = "topic"
	}
	return fmt.Sprintf("suggest/%d-%s-%s", tgID, slug, shortID)
}

// ImageExt returns the extension for an uploaded image from its file name
// or MIME type, or "" if it is not a supported format.
func ImageExt(fileName, mimeType string) string {
	ext := strings.ToLower(path.Ext(fileName))
	if ext == "" {
		switch mimeType {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		}
	}
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if ext != ".png" && ext != ".jpg" {
		return ""
	}
	return ext
}

// ImageName names the n-th picture of a suggestion: <slug>-<short_id>-<n>.
// The short ID keeps names unique across concurrent suggestions.
func ImageName(articlePath, shortID string, n int, ext string) string {
	slug := Slug(content.TitleFromFile(path.Base(articlePath)), 30)
	if slug == "" {
		slug = "image"
	}
	return fmt.Sprintf("%s-%s-%d%s", slug, shortID, n, ext)
}
