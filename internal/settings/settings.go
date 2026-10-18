// Package settings keeps the settings admins change while the bot runs:
// the rank ladder and the custom emoji. They live in settings.yml in the
// data directory, which the bot creates with defaults, rewrites from admin
// commands and rereads when it is edited by hand.
package settings

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Mimic890/terakoya/internal/ranks"
)

// FileName is the settings file in the data directory.
const FileName = "settings.yml"

// Settings is the content of settings.yml.
type Settings struct {
	Ranks []Rank `yaml:"ranks"`
	Emoji Emoji  `yaml:"emoji"`
}

// Rank is one step of the ladder.
type Rank struct {
	Title string `yaml:"title"`
	From  int    `yaml:"from"`
	Emoji string `yaml:"emoji"`
}

// Emoji holds custom emoji IDs; an empty ID means the plain emoji, or none.
type Emoji struct {
	KeyboardSubjects string            `yaml:"keyboard_subjects"`
	KeyboardPinned   string            `yaml:"keyboard_pinned"`
	KeyboardAccount  string            `yaml:"keyboard_account"`
	Pin              string            `yaml:"pin"`
	Greeting         string            `yaml:"greeting"`
	Subjects         map[string]string `yaml:"subjects"` // by subject folder name
}

// Default returns the settings of a new file: the default ladder and no
// custom emoji, with an entry for each of subjects.
func Default(subjects []string) Settings {
	l, _ := ranks.Parse(ranks.Default)
	return FromLadder(l, subjects)
}

// FromLadder returns settings with ladder l and no custom emoji.
func FromLadder(l ranks.Ladder, subjects []string) Settings {
	s := Settings{Emoji: Emoji{Subjects: map[string]string{}}}
	for _, r := range l {
		s.Ranks = append(s.Ranks, Rank{Title: r.Title, From: r.From, Emoji: r.Emoji})
	}
	for _, name := range subjects {
		s.Emoji.Subjects[name] = ""
	}
	return s
}

// Ladder returns the ranks with their emoji.
func (s Settings) Ladder() ranks.Ladder {
	l := make(ranks.Ladder, len(s.Ranks))
	for i, r := range s.Ranks {
		l[i] = ranks.Rank{Title: r.Title, From: r.From, Emoji: r.Emoji}
	}
	return l
}

// Clone returns a deep copy.
func (s Settings) Clone() Settings {
	out := s
	out.Ranks = append([]Rank{}, s.Ranks...)
	out.Emoji.Subjects = map[string]string{}
	for k, v := range s.Emoji.Subjects {
		out.Emoji.Subjects[k] = v
	}
	return out
}

// Validate checks the ladder and that every emoji ID is a number.
func (s Settings) Validate() error {
	if err := s.Ladder().Validate(); err != nil {
		return fmt.Errorf("ranks: %w", err)
	}
	check := func(where, id string) error {
		for _, r := range id {
			if r < '0' || r > '9' {
				return fmt.Errorf("%s: %q is not a custom emoji ID", where, id)
			}
		}
		return nil
	}
	for _, r := range s.Ranks {
		if err := check("rank "+r.Title, r.Emoji); err != nil {
			return err
		}
	}
	e := s.Emoji
	for where, id := range map[string]string{"keyboard_subjects": e.KeyboardSubjects, "keyboard_pinned": e.KeyboardPinned,
		"keyboard_account": e.KeyboardAccount, "pin": e.Pin, "greeting": e.Greeting} {
		if err := check(where, id); err != nil {
			return err
		}
	}
	for name, id := range e.Subjects {
		if err := check("subject "+name, id); err != nil {
			return err
		}
	}
	return nil
}

// Load reads and validates a settings file.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	for i := range s.Ranks {
		s.Ranks[i].Title = strings.TrimSpace(s.Ranks[i].Title)
	}
	if s.Emoji.Subjects == nil {
		s.Emoji.Subjects = map[string]string{}
	}
	if err := s.Validate(); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

const header = `# terakoya settings. Admin commands in the bot (/emoji_setup) rewrite this
# file; it can also be edited by hand: the bot rereads it within seconds
# and keeps the previous settings if the new ones are invalid (see its log).
#
# ranks: title, the number of accepted suggestions it starts at (the first
# starts at 0, then strictly increasing), and an optional custom emoji ID.
# emoji: custom emoji IDs; empty means the plain emoji, or none. subjects
# are keyed by the subject's folder name in the vault.

`

// Save writes the settings atomically, so a reader never sees half a file.
func Save(path string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.yml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SubjectNames lists the subjects with an emoji entry, sorted.
func (s Settings) SubjectNames() []string {
	var out []string
	for name := range s.Emoji.Subjects {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
