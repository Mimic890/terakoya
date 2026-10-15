package bot

import "testing"

func TestTrackerLastMenu(t *testing.T) {
	tr := newTracker()
	tr.seen(1, 10)    // user's /start
	tr.setMenu(1, 11) // menu
	if got := tr.lastMenu(1); got != 11 {
		t.Fatalf("lastMenu = %d, want 11", got)
	}
	tr.seen(1, 12) // article
	if got := tr.lastMenu(1); got != 0 {
		t.Fatalf("menu is not last, got %d", got)
	}
	tr.gone(1, 12) // article deleted
	if got := tr.lastMenu(1); got != 11 {
		t.Fatalf("after delete lastMenu = %d, want 11", got)
	}
	tr.gone(1, 11)
	if got := tr.lastMenu(1); got != 0 {
		t.Fatalf("deleted menu still returned: %d", got)
	}
	if got := tr.lastMenu(2); got != 0 {
		t.Fatalf("unknown chat lastMenu = %d", got)
	}
}

func TestTrackerOrderAndCap(t *testing.T) {
	tr := newTracker()
	for i := 1; i <= maxTracked+10; i++ {
		tr.seen(1, i)
	}
	tr.seen(1, 5) // late, already trimmed or duplicate
	tr.setMenu(1, maxTracked+10)
	c := tr.chats[1]
	if len(c.ids) > maxTracked {
		t.Fatalf("ids not capped: %d", len(c.ids))
	}
	for i := 1; i < len(c.ids); i++ {
		if c.ids[i] <= c.ids[i-1] {
			t.Fatalf("ids not ascending: %v", c.ids)
		}
	}
	if tr.lastMenu(1) != maxTracked+10 {
		t.Fatal("menu should be last")
	}
}
