package nick

import "testing"

func TestCheck(t *testing.T) {
	reserved := []string{"terakoya_bot", "Иван", "ivan_teacher"}
	cases := map[string]Problem{
		"Аня":           OK,
		"Звёздный_ёж-7": OK,
		"Mike Smith":    OK,
		"абв":           OK,
		"ab":            Length,
		"очень-длинный-никнейм": Length,
		"Аня!":          Chars,
		"emoji🙂":        Chars,
		"日本語です":         Chars,
		"tab\tname":     Chars,
		" Аня":          Spaces,
		"Аня ":          Spaces,
		"Ан  я":         Spaces,
		"Superadmin":    Reserved,
		"аdmin":         Reserved, // Cyrillic а
		"ad_min":        Reserved,
		"Главный Админ": Reserved,
		"TeraKoya fan":  Reserved,
		"terakoya bot":  Reserved,
		"ИванИванов":    Reserved,
		"ivan-teacher":  Reserved,
		"Ива":           OK, // contains no reserved name
		"fuckboy":       Banned,
		"xуйня":         Banned, // Latin x
		"Сукачёв":       Banned,
		"Пёс":           OK,
		"рубля":         OK,
	}
	for in, want := range cases {
		if got := Check(in, reserved); got != want {
			t.Errorf("Check(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKey(t *testing.T) {
	same := [][2]string{{"Ёжик", "ежик"}, {"Anya", "ANYA"}, {"Маша", "Mаша"}, {"Маша", "мaшa"}, {"Ма_ша", "Маша"},
		{"Ма ша", "Ма-ша"}, {"Bob0", "BOBO"}, {"Звезда", "3вeздa"}}
	for _, p := range same {
		if Key(p[0]) != Key(p[1]) {
			t.Errorf("%q and %q differ: %q, %q", p[0], p[1], Key(p[0]), Key(p[1]))
		}
	}
	for _, p := range [][2]string{{"Аня", "Anya"}, {"Маша", "Masha"}, {"Маша", "Маша2"}} {
		if Key(p[0]) == Key(p[1]) {
			t.Errorf("%q and %q share %q", p[0], p[1], Key(p[0]))
		}
	}
}
