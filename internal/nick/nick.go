// Package nick checks the nicknames users choose for themselves.
package nick

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Problem is why a nickname is refused; "" means it is fine.
type Problem string

const (
	OK       Problem = ""
	Length   Problem = "length"   // not 3 to 20 characters
	Chars    Problem = "chars"    // a character other than letters, digits, _, - and spaces
	Spaces   Problem = "spaces"   // a space at either end, or two in a row
	Banned   Problem = "banned"   // a rude word
	Reserved Problem = "reserved" // looks like an admin or the bot
)

// Length limits, in characters.
const (
	MinLen = 3
	MaxLen = 20
)

// reservedWords may not appear in any nickname.
var reservedWords = []string{"admin", "админ", "terakoya", "теракоя", "moderator", "модератор"}

// bannedWords is a short list of rude words, matched anywhere in a
// nickname, look-alike letters included.
var bannedWords = []string{
	"хуй", "хуе", "хуё", "хуя", "пизд", "ебат", "ебан", "ебал", "ебло", "уеб", "ёба", "бляд", "блят", "сука", "суки",
	"мудак", "мудил", "пидор", "пидар", "пидр", "залуп", "гандон", "шлюх", "дроч", "манда", "жопа",
	"fuck", "shit", "cunt", "bitch", "dick", "pussy", "nigg", "fag", "whore", "asshole", "slut", "porn",
}

// Check tells whether nick may be used. reserved are further names it
// must not contain, such as the bot's username and the admins' names.
func Check(nick string, reserved []string) Problem {
	if n := utf8.RuneCountInString(nick); n < MinLen || n > MaxLen {
		return Length
	}
	for _, r := range nick {
		if !allowed(r) {
			return Chars
		}
	}
	if strings.HasPrefix(nick, " ") || strings.HasSuffix(nick, " ") || strings.Contains(nick, "  ") {
		return Spaces
	}
	sk := skeleton(nick)
	for _, w := range bannedWords {
		if strings.Contains(sk, skeleton(w)) {
			return Banned
		}
	}
	for _, w := range append(append([]string{}, reservedWords...), reserved...) {
		if w = skeleton(w); utf8.RuneCountInString(w) >= MinLen && strings.Contains(sk, w) {
			return Reserved
		}
	}
	return OK
}

func allowed(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '_', r == '-', r == ' ':
		return true
	}
	return unicode.Is(unicode.Cyrillic, r) && unicode.IsLetter(r)
}

// Key is the form two nicknames share when they differ only in case;
// nicknames must be unique by it.
func Key(nick string) string {
	return strings.ReplaceAll(strings.ToLower(nick), "ё", "е")
}

// lookalike maps Cyrillic letters and digits that look like Latin
// letters to them, so that «аdmin» with a Cyrillic а is still admin.
var lookalike = map[rune]rune{
	'а': 'a', 'в': 'b', 'е': 'e', 'ё': 'e', 'к': 'k', 'м': 'm', 'н': 'h', 'о': 'o', 'р': 'p', 'с': 'c', 'т': 't',
	'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j', 'ѕ': 's', '0': 'o', '1': 'i', '3': 'з',
}

// skeleton lowercases s, folds look-alike letters together and drops
// separators, for matching words inside a nickname.
func skeleton(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '_', '-', '.', '@':
			continue
		}
		if l, ok := lookalike[r]; ok {
			r = l
		}
		b.WriteRune(r)
	}
	return b.String()
}
