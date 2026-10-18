package ranks

import "testing"

func TestDefault(t *testing.T) {
	l, err := Parse(Default)
	if err != nil {
		t.Fatal(err)
	}
	for merged, want := range map[int]string{0: "Novice", 1: "Pupil", 2: "Pupil", 3: "Student", 7: "Scholar", 14: "Scholar", 15: "Mentor",
		30: "Teacher", 59: "Teacher", 60: "Master", 100: "Sage", 500: "Sage"} {
		if got := l.For(merged).Title; got != want {
			t.Errorf("For(%d) = %q, want %q", merged, got, want)
		}
	}
}

func TestNext(t *testing.T) {
	l, _ := Parse(Default)
	if n, ok := l.Next(l.For(4)); !ok || n.Title != "Scholar" || n.From != 7 {
		t.Errorf("Next(Student) = %v, %v", n, ok)
	}
	if _, ok := l.Next(l.For(100)); ok {
		t.Error("Sage has a next rank")
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"", "a:1", "a", "a:0,b:x", "a:0,b:0", "a:-1", "a:0,a:1", ":0"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded", s)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, l := range []Ladder{
		{{Title: "a", From: 0}, {Title: "b", From: 2}, {Title: "c", From: 1}},
		{{Title: "a", From: 1}},
		{},
	} {
		if err := l.Validate(); err == nil {
			t.Errorf("%v accepted", l)
		}
	}
}
