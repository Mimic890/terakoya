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
	if Key("Ёжик") != Key("ежик") || Key("Anya") != Key("ANYA") || Key("Аня") == Key("Anya") {
		t.Error("keys")
	}
}
