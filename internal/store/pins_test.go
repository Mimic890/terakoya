package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPins(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	tick := int64(0)
	s.now = func() time.Time { tick++; return time.Unix(0, tick) }
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Pin(ctx, 42, id, 3); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Pin(ctx, 42, "d", 3); !errors.Is(err, ErrPinLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if err := s.Pin(ctx, 7, "a", 3); err != nil {
		t.Fatalf("other user: %v", err)
	}
	if err := s.Unpin(ctx, 42, "b"); err != nil {
		t.Fatal(err)
	}
	pins, _ := s.Pins(ctx, 42)
	if len(pins) != 2 || pins[0] != "c" || pins[1] != "a" {
		t.Fatalf("pins = %v, want newest first", pins)
	}
	if ok, _ := s.Pinned(ctx, 42, "a"); !ok {
		t.Error("a not pinned")
	}
	if ok, _ := s.Pinned(ctx, 42, "b"); ok {
		t.Error("b still pinned")
	}
}
