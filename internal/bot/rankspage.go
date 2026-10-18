package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"
)

// /ranks, and «Ранги» under «Аккаунт», list the ladder. A user sees the
// ranks they have reached and the next one in full; with
// ranks.hide_future, later ones show only their threshold. Admins see
// every rank and how to change them.

const cbRanks = "rk"

// ranksMarkdown is the ladder as the viewer sees it.
func (b *Bot) ranksMarkdown(ctx context.Context, from *models.User) string {
	u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
	}
	admin := b.cfg.IsAdmin(from.ID)
	hide := b.settings.Get().HideFutureRanks && !admin
	ladder := b.ladder()
	current := ladder.For(u.Accepted)
	var lines []string
	next := false // the next rank was listed
	for _, r := range ladder {
		from := plural(r.From, b.t.AcceptedFrom)
		if hide && r.From > u.Accepted && next {
			lines = append(lines, escapeMarkdown(fmt.Sprintf(b.t.RanksHidden, r.From, from)))
			continue
		}
		if r.From > u.Accepted {
			next = true
		}
		icon := ""
		if id := b.emoji.Rank(r.Title); id != "" {
			icon = emojiRef(id, "🏅") + " "
		}
		title := escapeMarkdown(r.Title)
		// The title goes in after escaping, so that it can be bold.
		line := escapeMarkdown(fmt.Sprintf(b.t.RanksLine, "\x00", r.From, from))
		if r.Title == current.Title {
			line = strings.Replace(line, "\x00", "**"+title+"**", 1) + escapeMarkdown(b.t.RanksYours)
		} else {
			line = strings.Replace(line, "\x00", title, 1)
		}
		lines = append(lines, icon+line)
	}
	md := "# " + b.t.RanksTitle + "\n\n" + strings.Join(lines, "  \n") + "\n"
	if admin {
		md += "\n" + escapeMarkdown(b.t.RanksEdit) + "\n"
	}
	return md
}

func (b *Bot) ranksKeyboard() *models.InlineKeyboardMarkup {
	return keyboard(row(button(b.t.Back, cbAccount), button(b.t.Close, cbClose)))
}

// showRanks replaces the current menu with the ladder.
func (b *Bot) showRanks(ctx context.Context, chatID int64, from *models.User) {
	b.showRich(ctx, chatID, b.t.RanksTitle, b.ranksMarkdown(ctx, from), b.ranksKeyboard())
}
