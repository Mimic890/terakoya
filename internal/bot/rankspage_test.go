package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/settings"
)

func ranksHarness(t *testing.T) *harness {
	h := newHarness(t, nil)
	h.setRanks("Novice:0,Pupil:1,Student:3,Scholar:7")
	h.tg.emoji = []string{"501", "503"}
	h.bot.settings.Update(func(s *settings.Settings) { s.Ranks[0].Emoji, s.Ranks[2].Emoji = "501", "503" })
	h.tg.take()
	return h
}

func TestRanksPage(t *testing.T) {
	h := ranksHarness(t)

	// No accepted правки: Novice, the next one in full, the rest hidden.
	want := "# Ранги\n\n" +
		"![🏅](tg://emoji?id=501) **Novice** — от 0 принятых правок · ваш ранг  \n" +
		"Pupil — от 1 принятой правки  \n" +
		"??? — от 3 принятых правок  \n" +
		"??? — от 7 принятых правок\n"
	if got := h.bot.ranksMarkdown(h.ctx, &anya); got != want {
		t.Errorf("new user:\n%q\nwant\n%q", got, want)
	}

	// Two: Pupil reached, Student next with its emoji, Scholar hidden.
	h.accept(anya, 2)
	want = "# Ранги\n\n" +
		"![🏅](tg://emoji?id=501) Novice — от 0 принятых правок  \n" +
		"**Pupil** — от 1 принятой правки · ваш ранг  \n" +
		"![🏅](tg://emoji?id=503) Student — от 3 принятых правок  \n" +
		"??? — от 7 принятых правок\n"
	if got := h.bot.ranksMarkdown(h.ctx, &anya); got != want {
		t.Errorf("two:\n%q\nwant\n%q", got, want)
	}

	// At the top rank everything shows.
	h.accept(anya, 5)
	if got := h.bot.ranksMarkdown(h.ctx, &anya); strings.Contains(got, "???") || !strings.Contains(got, "**Scholar** — от 7 принятых правок · ваш ранг\n") {
		t.Errorf("top rank:\n%s", got)
	}

	// hide_future: false shows every rank to everyone.
	h.bot.settings.Update(func(s *settings.Settings) { s.HideFutureRanks = false })
	if got := h.bot.ranksMarkdown(h.ctx, &boris); strings.Contains(got, "???") || !strings.Contains(got, "Scholar — от 7") ||
		strings.Contains(got, "settings.yml") {
		t.Errorf("hide_future off:\n%s", got)
	}
}

func TestRanksPageAdmin(t *testing.T) {
	h := ranksHarness(t)
	got := h.bot.ranksMarkdown(h.ctx, &ivan)
	if strings.Contains(got, "???") || !strings.Contains(got, "Scholar — от 7 принятых правок\n") ||
		!strings.Contains(got, "**Novice** — от 0 принятых правок · ваш ранг") ||
		!strings.HasSuffix(got, "\n\n"+escapeMarkdown(i18n.RU.RanksEdit)+"\n") {
		t.Errorf("admin:\n%s", got)
	}
}

func TestRanksMessages(t *testing.T) {
	h := ranksHarness(t)
	h.say("/start")
	h.tg.take()

	// /ranks is for everyone: a rich menu with «Назад» and «Закрыть», and
	// the command goes.
	h.say("/ranks")
	calls := h.tg.take()
	r := lastRich(t, calls)
	if !strings.Contains(r.params["rich_message"], "# Ранги") ||
		r.params["reply_markup"] != `{"inline_keyboard":[[{"text":"« Назад","callback_data":"ac"},{"text":"Закрыть","callback_data":"c"}]]}` {
		t.Errorf("ranks = %+v", r.params)
	}
	if !contains(ids(calls, "deleteMessage"), fmt.Sprint(h.msgID)) {
		t.Errorf("command not deleted: %v", ids(calls, "deleteMessage"))
	}

	// «Аккаунт» has «Ранги» next to «Мои правки» and «Топ».
	h.say(i18n.RU.KeyAccount)
	acc := lastRich(t, h.tg.take())
	if !strings.Contains(acc.params["reply_markup"],
		`[{"text":"Мои правки","callback_data":"hl"},{"text":"Топ","callback_data":"tp"},{"text":"Ранги","callback_data":"rk"}]`) {
		t.Fatalf("account = %s", acc.params["reply_markup"])
	}
	accID := h.tg.lastID("sendRichMessage")
	h.tapOn(accID, cbRanks)
	calls = h.tg.take()
	if len(richs(calls)) != 1 || !contains(ids(calls, "deleteMessage"), fmt.Sprint(accID)) {
		t.Errorf("ranks from account = %+v", calls)
	}
}
