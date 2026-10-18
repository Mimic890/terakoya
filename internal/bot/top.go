package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/settings"
)

// /top, and «Топ» under «Аккаунт», rank users by accepted reports. Users
// who hide from the top are left out for everyone else but still see
// their own place.

const cbTop = "tp"

// topRow is one place of the top.
type topRow struct {
	place    int
	name     string
	accepted int
	rank     string // the rank's title
	rankIcon string // the rank's custom emoji, or ""
	medal    string // the place's custom emoji, or ""
}

// medalAlts stand in for medals where custom emoji cannot be drawn.
var medalAlts = []string{"🥇", "🥈", "🥉"}

// emojiRef is a custom emoji in rich Markdown, without a space after it.
func emojiRef(id, alt string) string { return "![" + alt + "](tg://emoji?id=" + id + ")" }

// topLine formats a place in rich Markdown, as set by top: in
// settings.yml:
//
//	none         1. name — 12
//	title        1. name — Scholar · 12
//	emoji        🥇 name 🎓 · 12
//	title_emoji  🥇 name — 🎓 Scholar · 12
//
// A rank without a custom emoji shows its title instead; a medal place
// without one shows its number; without show_count the count goes.
func topLine(st settings.Top, r topRow) string {
	var sb strings.Builder
	if r.medal != "" && r.place <= st.Medals {
		alt := "🏅"
		if r.place <= len(medalAlts) {
			alt = medalAlts[r.place-1]
		}
		sb.WriteString(emojiRef(r.medal, alt) + " ")
	} else {
		// Escaped, so that the lines do not become a numbered list.
		fmt.Fprintf(&sb, "%d\\. ", r.place)
	}
	sb.WriteString(escapeMarkdown(r.name))
	style := st.RankStyle
	if r.rankIcon == "" && (style == settings.RankEmoji || style == settings.RankTitleEmoji) {
		style = settings.RankTitle
	}
	title := escapeMarkdown(r.rank)
	sep := " · "
	switch style {
	case settings.RankNone:
		sep = " — "
	case settings.RankTitle:
		sb.WriteString(" — " + title)
	case settings.RankEmoji:
		sb.WriteString(" " + emojiRef(r.rankIcon, "🏅"))
	case settings.RankTitleEmoji:
		sb.WriteString(" — " + emojiRef(r.rankIcon, "🏅") + " " + title)
	}
	if st.ShowCount {
		fmt.Fprintf(&sb, "%s%d", sep, r.accepted)
	}
	return sb.String()
}

// topMarkdown is the top as viewer sees it.
func (b *Bot) topMarkdown(ctx context.Context, viewer *models.User) string {
	all, err := b.store.Leaderboard(ctx)
	if err != nil {
		b.log.Error("leaderboard", "err", err)
	}
	s := b.settings.Get()
	ladder := b.ladder()
	var lines []string
	// Places count the users everyone sees; the viewer's own place is
	// where they would stand among them.
	place, mine, mineCount, mineHidden := 0, 0, 0, false
	for _, e := range all {
		if e.TgID == viewer.ID {
			mine, mineCount, mineHidden = place+1, e.Accepted, e.HideTop
		}
		if e.HideTop {
			continue
		}
		place++
		if place > s.Top.Size {
			continue
		}
		rank := ladder.For(e.Accepted).Title
		lines = append(lines, topLine(s.Top, topRow{place: place, name: e.DisplayName(), accepted: e.Accepted,
			rank: rank, rankIcon: b.emoji.Rank(rank), medal: b.emoji.Medal(place)}))
	}
	var sb strings.Builder
	sb.WriteString("# " + b.t.TopTitle + "\n\n")
	if len(lines) == 0 {
		sb.WriteString(b.t.TopEmpty + "\n")
	} else {
		// Two trailing spaces break the line within a paragraph.
		sb.WriteString(strings.Join(lines, "  \n") + "\n")
	}
	switch {
	case mine != 0 && mine <= s.Top.Size && !mineHidden:
		// In the list already.
	case mine != 0:
		sb.WriteString("\n" + fmt.Sprintf(b.t.TopYou, mine, mineCount, plural(mineCount, b.t.Reports)) + "\n")
	case len(lines) > 0:
		sb.WriteString("\n" + b.t.TopYouNone + "\n")
	}
	return sb.String()
}

func (b *Bot) topKeyboard() *models.InlineKeyboardMarkup {
	return keyboard(row(button(b.t.Back, cbAccount), button(b.t.Close, cbClose)))
}

// showTop replaces the current menu with the top.
func (b *Bot) showTop(ctx context.Context, chatID int64, from *models.User) {
	if _, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	}
	b.showRich(ctx, chatID, b.t.TopTitle, b.topMarkdown(ctx, from), b.topKeyboard())
}
