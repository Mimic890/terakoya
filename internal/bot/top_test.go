package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/store"
)

func TestTopLineEveryCombination(t *testing.T) {
	const rankRef = "![🏅](tg://emoji?id=R)"
	// The part after the name, by style and whether the rank has an emoji:
	// without and with the count.
	type after struct{ bare, counted string }
	bodies := map[string][2]after{ // [no rank emoji, rank emoji]
		settings.RankNone:       {{"", " — 12"}, {"", " — 12"}},
		settings.RankTitle:      {{" — Scholar", " — Scholar · 12"}, {" — Scholar", " — Scholar · 12"}},
		settings.RankEmoji:      {{" — Scholar", " — Scholar · 12"}, {" " + rankRef, " " + rankRef + " · 12"}},
		settings.RankTitleEmoji: {{" — Scholar", " — Scholar · 12"}, {" — " + rankRef + " Scholar", " — " + rankRef + " Scholar · 12"}},
	}
	medals := []struct {
		name   string
		medal  string
		medals int
		prefix string
	}{
		{"medal", "M", 3, "![🥇](tg://emoji?id=M) "},
		{"medal beyond top.medals", "M", 0, "1\\. "},
		{"no medal emoji", "", 3, "1\\. "},
	}
	n := 0
	for style, body := range bodies {
		for _, icon := range []string{"", "R"} {
			for _, count := range []bool{false, true} {
				for _, m := range medals {
					st := settings.Top{Size: 20, Medals: m.medals, RankStyle: style, ShowCount: count}
					got := topLine(st, topRow{place: 1, name: "Аня", accepted: 12, rank: "Scholar", rankIcon: icon, medal: m.medal})
					a := body[0]
					if icon != "" {
						a = body[1]
					}
					want := m.prefix + "Аня" + a.bare
					if count {
						want = m.prefix + "Аня" + a.counted
					}
					name := fmt.Sprintf("%s icon=%q count=%v %s", style, icon, count, m.name)
					if got != want {
						t.Errorf("%s:\n got %q\nwant %q", name, got, want)
					}
					if strings.Contains(got, "  ") || strings.HasSuffix(got, " ") || strings.HasSuffix(got, "—") ||
						strings.HasSuffix(got, "·") || strings.Contains(got, "— ·") || strings.Contains(got, "· —") {
						t.Errorf("%s: stray space or separator in %q", name, got)
					}
					n++
				}
			}
		}
	}
	if n != 48 {
		t.Errorf("%d combinations", n)
	}
	// Places past the third medal stand in with a plain medal; names are
	// literal.
	st := settings.Top{Size: 20, Medals: 5, RankStyle: settings.RankNone, ShowCount: true}
	if got := topLine(st, topRow{place: 4, name: "*Звезда*", accepted: 1, medal: "M4"}); got != "![🏅](tg://emoji?id=M4) \\*Звезда\\* — 1" {
		t.Errorf("fourth = %q", got)
	}
	if got := topLine(st, topRow{place: 12, name: "Аня", accepted: 1}); got != "12\\. Аня — 1" {
		t.Errorf("twelfth = %q", got)
	}
}

// accept gives a user n accepted reports.
func (h *harness) accept(u models.User, n int) {
	h.t.Helper()
	h.store.TouchUser(h.ctx, u.ID, u.Username, u.FirstName)
	for i := 0; i < n; i++ {
		h.msgID++
		sg := &store.Suggestion{ShortID: fmt.Sprintf("t%d", h.msgID), TgID: u.ID, Kind: store.KindReport, Issue: 1000 + h.msgID}
		if err := h.store.CreateSuggestion(h.ctx, sg); err != nil {
			h.t.Fatal(err)
		}
		h.store.ResolveSuggestion(h.ctx, sg.ID, store.StatusMerged)
	}
}

func TestTop(t *testing.T) {
	h := newHarness(t, nil)
	olga := models.User{ID: 44, FirstName: "Ольга"}
	h.bot.settings.Update(func(s *settings.Settings) {
		s.Top = settings.Top{Size: 2, Medals: 1, RankStyle: settings.RankTitle, ShowCount: true}
	})
	if got := h.bot.topMarkdown(h.ctx, &anya); got != "# Топ участников\n\nПока здесь никого нет. Сюда попадают те, чьи правки приняли.\n" {
		t.Errorf("empty = %q", got)
	}

	h.accept(boris, 3)
	h.accept(olga, 1)
	h.accept(anya, 1) // after Ольга: lower on the tie
	h.store.TouchUser(h.ctx, ivan.ID, ivan.Username, ivan.FirstName)

	// Аня is third, past the two places: her own line comes last.
	got := h.bot.topMarkdown(h.ctx, &anya)
	want := "# Топ участников\n\n1\\. @boris — senpai · 3  \n2\\. Ольга — senpai · 1\n\nВаше место: 3 — 1 принятая правка\n"
	if got != want {
		t.Errorf("anya sees\n%q\nwant\n%q", got, want)
	}
	// Иван has none.
	if got := h.bot.topMarkdown(h.ctx, &ivan); !strings.HasSuffix(got, "\n\nВас пока нет в топе: вы попадёте сюда, когда примут вашу первую правку.\n") {
		t.Errorf("ivan sees %q", got)
	}
	// Борис is in the list: no line of his own.
	if got := h.bot.topMarkdown(h.ctx, &boris); strings.Contains(got, "Ваше место") {
		t.Errorf("boris sees %q", got)
	}

	// Борис hides: the others move up, and only he sees his place.
	h.store.SetHideTop(h.ctx, boris.ID, true)
	got = h.bot.topMarkdown(h.ctx, &anya)
	if got != "# Топ участников\n\n1\\. Ольга — senpai · 1  \n2\\. @anya — senpai · 1\n" {
		t.Errorf("anya after boris hid: %q", got)
	}
	got = h.bot.topMarkdown(h.ctx, &boris)
	if strings.Contains(got, "@boris") || !strings.HasSuffix(got, "\n\nВаше место: 1 — 3 принятые правки\n") {
		t.Errorf("boris after hiding: %q", got)
	}
}

func TestTopMessages(t *testing.T) {
	h := newHarness(t, nil)
	h.accept(anya, 2)
	h.say("/start")
	h.tg.take()

	// /top is a rich menu with «Назад» to «Аккаунт» and «Закрыть», and the
	// command goes.
	h.say("/top")
	calls := h.tg.take()
	top := lastRich(t, calls)
	if !strings.Contains(top.params["rich_message"], "# Топ участников") || !strings.Contains(top.params["rich_message"], "1\\\\. @anya") ||
		!strings.Contains(top.params["reply_markup"], `"callback_data":"ac"`) || !strings.Contains(top.params["reply_markup"], `"callback_data":"c"`) {
		t.Errorf("top = %+v", top.params)
	}
	if !contains(ids(calls, "deleteMessage"), fmt.Sprint(h.msgID)) {
		t.Errorf("command not deleted: %v", ids(calls, "deleteMessage"))
	}

	// «Аккаунт» has «Топ»; it replaces the account.
	h.say(i18n.RU.KeyAccount)
	acc := lastRich(t, h.tg.take())
	if !strings.Contains(acc.params["reply_markup"], `"Топ","callback_data":"tp"`) {
		t.Fatalf("account = %s", acc.params["reply_markup"])
	}
	accID := h.tg.lastID("sendRichMessage")
	h.tapOn(accID, cbTop)
	calls = h.tg.take()
	if len(richs(calls)) != 1 || !contains(ids(calls, "deleteMessage"), fmt.Sprint(accID)) {
		t.Errorf("top from account = %+v", calls)
	}
}
