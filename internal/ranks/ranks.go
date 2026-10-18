// Package ranks maps the number of merged suggestions to a title.
package ranks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Rank is a title reached at a number of merged suggestions.
type Rank struct {
	Title string
	From  int
}

// Ladder is a list of ranks sorted by threshold.
type Ladder []Rank

// Default is used when no ranks are configured.
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
	if l[0].From != 0 {
		return nil, fmt.Errorf("the first rank must start at 0")
	}
	for i := 1; i < len(l); i++ {
		if l[i].From == l[i-1].From {
			return nil, fmt.Errorf("ranks %q and %q share threshold %d", l[i-1].Title, l[i].Title, l[i].From)
		}
	}
	return l, nil
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
