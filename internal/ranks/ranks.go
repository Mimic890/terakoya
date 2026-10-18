// Package ranks maps the number of merged suggestions to a title.
package ranks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Rank is a title reached at a number of merged suggestions. Emoji is
// the custom emoji ID shown with it, if any.
type Rank struct {
	Title string
	From  int
	Emoji string
}

// Ladder is a list of ranks sorted by threshold.
type Ladder []Rank

// Default is the ladder of a new settings file.
const Default = "Novice:0,Pupil:1,Student:3,Scholar:7,Mentor:15,Teacher:30,Master:60,Sage:100"

// Parse reads "title:threshold,title:threshold,…". The lowest threshold
// must be 0 so every user has a rank.
func Parse(s string) (Ladder, error) {
	var l Ladder
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		i := strings.LastIndexByte(part, ':')
		if i <= 0 {
			return nil, fmt.Errorf("rank %q: want title:threshold", part)
		}
		n, err := strconv.Atoi(strings.TrimSpace(part[i+1:]))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("rank %q: threshold must be a non-negative integer", part)
		}
		l = append(l, Rank{Title: strings.TrimSpace(part[:i]), From: n})
	}
	if len(l) == 0 {
		return nil, fmt.Errorf("no ranks defined")
	}
	sort.SliceStable(l, func(i, j int) bool { return l[i].From < l[j].From })
	return l, l.Validate()
}

// Validate checks that the ladder starts at 0, its thresholds strictly
// increase, and every title is set and unique.
func (l Ladder) Validate() error {
	if len(l) == 0 {
		return fmt.Errorf("no ranks defined")
	}
	if l[0].From != 0 {
		return fmt.Errorf("the first rank must start at 0")
	}
	seen := map[string]bool{}
	for i, r := range l {
		if strings.TrimSpace(r.Title) == "" {
			return fmt.Errorf("rank %d has no title", i+1)
		}
		if seen[r.Title] {
			return fmt.Errorf("rank %q appears twice", r.Title)
		}
		seen[r.Title] = true
		if i > 0 && r.From <= l[i-1].From {
			return fmt.Errorf("rank %q starts at %d, not after %q at %d", r.Title, r.From, l[i-1].Title, l[i-1].From)
		}
	}
	return nil
}

// Next returns the rank after r, and false at the top.
func (l Ladder) Next(r Rank) (Rank, bool) {
	for _, x := range l {
		if x.From > r.From {
			return x, true
		}
	}
	return Rank{}, false
}

// For returns the rank for a number of merged suggestions.
func (l Ladder) For(merged int) Rank {
	r := l[0]
	for _, x := range l {
		if merged >= x.From {
			r = x
		}
	}
	return r
}
