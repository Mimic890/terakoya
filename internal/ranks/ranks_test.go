package ranks

import "testing"

func TestDefault(t *testing.T) {
	l, err := Parse(Default)
	if err != nil {
		t.Fatal(err)
	}
	for merged, want := range map[int]string{0: "kōhai", 2: "kōhai", 3: "senpai", 9: "senpai", 10: "sensei", 500: "sensei"} {
		if got := l.For(merged).Title; got != want {
			t.Errorf("For(%d) = %q, want %q", merged, got, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"", "a:1", "a", "a:0,b:x", "a:0,b:0", "a:-1"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded", s)
		}
	}
}
