package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNicknames(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.TouchUser(ctx, 42, "anya", "Аня")
	s.TouchUser(ctx, 43, "", "Борис")
	if u, _ := s.User(ctx, 42); u.DisplayName() != "@anya" {
		t.Errorf("display = %q", u.DisplayName())
	}
	if u, _ := s.User(ctx, 43); u.DisplayName() != "Борис" {
		t.Errorf("display = %q", u.DisplayName())
	}
	if err := s.SetNickname(ctx, 42, "Звезда", "звезда"); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.User(ctx, 42); u.Nickname != "Звезда" || u.DisplayName() != "Звезда" {
		t.Errorf("nickname = %+v", u)
	}
	if err := s.SetNickname(ctx, 43, "звезда", "звезда"); !errors.Is(err, ErrNicknameTaken) {
		t.Errorf("duplicate: %v", err)
	}
	if taken, _ := s.NicknameTaken(ctx, 43, "звезда"); !taken {
		t.Error("not taken for another user")
	}
	if taken, _ := s.NicknameTaken(ctx, 42, "звезда"); taken {
		t.Error("taken by the user themselves")
	}
	// Clearing frees it; several users may have none.
	s.SetNickname(ctx, 42, "", "")
	if err := s.SetNickname(ctx, 43, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNickname(ctx, 43, "звезда", "звезда"); err != nil {
		t.Errorf("after clearing: %v", err)
	}
	if u, err := s.UserByUsername(ctx, "@ANYA"); err != nil || u.TgID != 42 {
		t.Errorf("by username = %+v, %v", u, err)
	}
}

func TestLeaderboard(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := int64(1000)
	s.now = func() time.Time { return time.Unix(now, 0) }
	accept := func(user int64, n int) {
		s.TouchUser(ctx, user, "", "")
		for i := 0; i < n; i++ {
			now++
			sg := &Suggestion{ShortID: time.Unix(now, 0).String(), TgID: user, Kind: KindReport, PRNumber: int(now)}
			s.CreateSuggestion(ctx, sg)
			s.ResolveSuggestion(ctx, sg.ID, StatusMerged)
		}
	}
	accept(1, 2)
	accept(2, 3)
	accept(3, 2) // reaches 2 after user 1
	s.TouchUser(ctx, 4, "", "")
	edit := &Suggestion{ShortID: "e", TgID: 4, Kind: KindEdit, PRNumber: 1}
	s.CreateSuggestion(ctx, edit)
	s.ResolveSuggestion(ctx, edit.ID, StatusMerged)
	s.SetHideTop(ctx, 3, true)

	top, err := s.Leaderboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, e := range top {
		got = append(got, e.TgID)
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 1 || got[2] != 3 || top[0].Accepted != 3 || !top[2].HideTop {
		t.Errorf("top = %+v", top)
	}
}

func TestRecipientsAndBroadcasts(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for _, id := range []int64{1, 2, 3, 7} {
		s.TouchUser(ctx, id, "", "")
	}
	s.MarkBlocked(ctx, 2)
	got, err := s.Recipients(ctx, []int64{7})
	if err != nil || len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("recipients = %v, %v", got, err)
	}
	// Using the bot again unblocks.
	s.TouchUser(ctx, 2, "", "")
	if got, _ := s.Recipients(ctx, nil); len(got) != 4 {
		t.Errorf("after unblock = %v", got)
	}

	b := &Broadcast{AdminID: 7, Markdown: "**Привет**"}
	if err := s.CreateBroadcast(ctx, b, []int64{1, 2, 3}); err != nil || b.ID == 0 || b.Recipients != 3 {
		t.Fatalf("create = %+v, %v", b, err)
	}
	b.Sent, b.Blocked = 2, 1
	s.UpdateBroadcast(ctx, b, false)
	all, _ := s.Broadcasts(ctx)
	if len(all) != 1 || all[0].Sent != 2 || !all[0].Finished.IsZero() {
		t.Errorf("in progress = %+v", all)
	}
	s.UpdateBroadcast(ctx, b, true)
	all, _ = s.Broadcasts(ctx)
	if all[0].Finished.IsZero() || all[0].Markdown != "**Привет**" || all[0].Blocked != 1 || all[0].Recipients != 3 {
		t.Errorf("finished = %+v", all)
	}
}

func TestBroadcastResume(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	b := &Broadcast{AdminID: 7, Markdown: "x"}
	if err := s.CreateBroadcast(ctx, b, []int64{5, 1, 4, 2, 3}); err != nil {
		t.Fatal(err)
	}
	s.SetBroadcastProgress(ctx, b, 7, 900)
	if got, _ := s.PendingRecipients(ctx, b.ID); fmt.Sprint(got) != "[1 2 3 4 5]" {
		t.Fatalf("pending = %v", got)
	}
	s.MarkRecipient(ctx, b.ID, 1, RecipientSent)
	s.MarkRecipient(ctx, b.ID, 2, RecipientBlocked)
	// The bot stopped while sending to 3.
	s.MarkRecipient(ctx, b.ID, 3, RecipientSending)

	open, err := s.UnfinishedBroadcasts(ctx)
	if err != nil || len(open) != 1 || open[0].ID != b.ID || open[0].ProgressChat != 7 || open[0].ProgressMsg != 900 {
		t.Fatalf("unfinished = %+v, %v", open, err)
	}
	r := open[0]
	if err := s.ResumeBroadcast(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if r.Sent != 1 || r.Blocked != 1 || r.Failed != 1 {
		t.Errorf("counts = %+v", r)
	}
	// 3 may have it already and is not sent to again.
	if got, _ := s.PendingRecipients(ctx, b.ID); fmt.Sprint(got) != "[4 5]" {
		t.Errorf("pending after resume = %v", got)
	}
	s.UpdateBroadcast(ctx, &r, true)
	if open, _ := s.UnfinishedBroadcasts(ctx); len(open) != 0 {
		t.Errorf("still unfinished = %+v", open)
	}
}
