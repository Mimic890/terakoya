package content

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// numberPrefix matches the order prefix of a topic folder: digits followed
// by a space, ".", "_" or "-".
var numberPrefix = regexp.MustCompile(`^(\d+)[ ._-]+`)

// ParseNumber splits "05 Параллельность" into 5, "Параллельность" and the
// width 2. ok is false if there is no prefix.
func ParseNumber(name string) (n int, rest string, width int, ok bool) {
	m := numberPrefix.FindStringSubmatch(name)
	if m == nil {
		return 0, name, 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, name, 0, false
	}
	return n, name[len(m[0]):], len(m[1]), true
}

func stripNumber(name string) string {
	_, rest, _, _ := ParseNumber(name)
	return rest
}

var gradeName = regexp.MustCompile(`^(\d+) класс$`)

// ParseGrade returns N for a folder named "<N> класс".
func ParseGrade(name string) (int, bool) {
	m := gradeName.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// Kind folders with known singular labels.
var kindLabels = map[string]string{
	"теоремы":     "Теорема",
	"аксиомы":     "Аксиома",
	"определения": "Определение",
	"задачи":      "Задача",
	"следствия":   "Следствие",
}

// Kinds a new note can be created as, in menu order, with their folders.
var Kinds = []string{"теоремы", "аксиомы", "определения", "задачи", "следствия"}

// KindLabel returns the label of a kind folder: «Теорема» for теоремы, the
// capitalized folder name for folders it does not know.
func KindLabel(kind string) string {
	if l, ok := kindLabels[strings.ToLower(kind)]; ok {
		return l
	}
	return capitalize(kind)
}

// KindHeading is the main page section for a kind: «Теоремы».
func KindHeading(kind string) string { return capitalize(kind) }

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

var trailingParen = regexp.MustCompile(`\s*\([^()]*\)\s*$`)

// TitleFromFile is the title of a note without an H1: the file name
// without extension, order prefix and trailing "(…)".
func TitleFromFile(name string) string {
	name = strings.TrimSuffix(name, ".md")
	name = stripNumber(name)
	if t := strings.TrimSpace(trailingParen.ReplaceAllString(name, "")); t != "" {
		return t
	}
	return strings.TrimSpace(name)
}

var digits = regexp.MustCompile(`\d+`)

// sortHint returns the numbers in the trailing "(…)" of a file name, so
// (5.2) sorts before (5.10) and (задача 2) before (задача 10).
func sortHint(name string) []int {
	m := trailingParen.FindString(strings.TrimSuffix(name, ".md"))
	var out []int
	for _, d := range digits.FindAllString(m, -1) {
		n, _ := strconv.Atoi(d)
		out = append(out, n)
	}
	return out
}

func lessHint(a, b []int) (less, decided bool) {
	if len(a) == 0 || len(b) == 0 {
		if len(a) == len(b) {
			return false, false
		}
		return len(a) > 0, true // numbered notes first
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i], true
		}
	}
	if len(a) != len(b) {
		return len(a) < len(b), true
	}
	return false, false
}

// ID derives a short stable ID from a vault path: the first 66 bits of the
// SHA-256 of the path with the numeric prefixes of its parts removed, in
// URL-safe base64. Renumbering topics keeps IDs; renaming changes them.
func ID(rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = stripNumber(p)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "/")))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:11]
}

// illegalName are characters Windows or macOS do not allow in file names.
var illegalName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SafeFileName turns a title into a file name stem that works on Windows
// and macOS: illegal characters removed, spaces collapsed, no trailing dots
// or spaces.
func SafeFileName(title string) string {
	s := illegalName.ReplaceAllString(title, "")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, ". ")
	if r := []rune(s); len(r) > 120 {
		s = strings.TrimRight(string(r[:120]), ". ")
	}
	return s
}
