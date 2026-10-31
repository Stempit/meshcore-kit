package companion

import (
	"fmt"
	"testing"
)

func TestInboxDrainReturnsMessagesInOrderAndClears(t *testing.T) {
	in := &Inbox{}
	in.Add(Message{Type: "dm", Text: "first"})
	in.Add(Message{Type: "channel", Text: "second"})

	msgs := in.Drain()
	if len(msgs) != 2 {
		t.Fatalf("Drain returned %d messages, want 2", len(msgs))
	}
	if msgs[0].Text != "first" || msgs[1].Text != "second" {
		t.Errorf("messages out of order: %q, %q", msgs[0].Text, msgs[1].Text)
	}
	if msgs[0].ID == 0 || msgs[1].ID <= msgs[0].ID {
		t.Errorf("IDs not sequential: %d then %d", msgs[0].ID, msgs[1].ID)
	}
	if msgs[0].ReceivedAt.IsZero() {
		t.Error("Add did not stamp ReceivedAt")
	}

	if got := in.Len(); got != 0 {
		t.Errorf("Len after Drain = %d, want 0", got)
	}
	if again := in.Drain(); len(again) != 0 {
		t.Errorf("second Drain returned %d messages, want 0", len(again))
	}
}

func TestInboxDrainNeverReturnsNil(t *testing.T) {
	in := &Inbox{}
	if msgs := in.Drain(); msgs == nil {
		t.Fatal("Drain on empty inbox returned nil, want empty slice")
	}
}

func TestInboxDropsOldestWhenFull(t *testing.T) {
	in := &Inbox{}
	for i := 0; i < maxInboxSize+5; i++ {
		in.Add(Message{Text: string(rune('a' + i%26))})
	}
	if got := in.Len(); got != maxInboxSize {
		t.Fatalf("Len = %d, want %d", got, maxInboxSize)
	}
	msgs := in.Drain()
	if msgs[0].ID != 6 {
		t.Errorf("oldest surviving message ID = %d, want 6 (first 5 dropped)", msgs[0].ID)
	}
}

func TestInboxIDsContinueAcrossDrains(t *testing.T) {
	in := &Inbox{}
	first := in.Add(Message{Text: "a"})
	in.Drain()
	second := in.Add(Message{Text: "b"})
	if second.ID <= first.ID {
		t.Errorf("ID after drain (%d) not greater than before (%d)", second.ID, first.ID)
	}
}

func TestInboxSinceReturnsNewerWithoutRemoving(t *testing.T) {
	in := &Inbox{}
	for i := 0; i < 5; i++ {
		in.Add(Message{Text: fmt.Sprintf("m%d", i)})
	}

	msgs := in.Since(3)
	if len(msgs) != 2 {
		t.Fatalf("Since(3) returned %d messages, want 2", len(msgs))
	}
	if msgs[0].Text != "m3" || msgs[1].Text != "m4" {
		t.Errorf("unexpected messages: %q, %q", msgs[0].Text, msgs[1].Text)
	}
	if got := in.Len(); got != 5 {
		t.Errorf("Since removed messages: Len = %d, want 5", got)
	}
	if all := in.Since(0); len(all) != 5 {
		t.Errorf("Since(0) returned %d messages, want 5", len(all))
	}
}

func TestInboxSinceNeverReturnsNil(t *testing.T) {
	in := &Inbox{}
	if msgs := in.Since(0); msgs == nil {
		t.Fatal("Since on empty inbox returned nil, want empty slice")
	}
}

func TestInboxLastID(t *testing.T) {
	in := &Inbox{}
	if got := in.LastID(); got != 0 {
		t.Errorf("LastID on empty inbox = %d, want 0", got)
	}
	a := in.Add(Message{Text: "a"})
	in.Add(Message{Text: "b"})
	if got := in.LastID(); got != a.ID+1 {
		t.Errorf("LastID = %d, want %d", got, a.ID+1)
	}
	// LastID is a safe cursor: an ID observed before a message exists is
	// strictly less than that message's ID.
	in.Drain()
	cursor := in.LastID()
	next := in.Add(Message{Text: "c"})
	if next.ID <= cursor {
		t.Errorf("message after cursor not newer: %d <= %d", next.ID, cursor)
	}
}

func TestInboxSinceWithLastID(t *testing.T) {
	in := &Inbox{}
	if msgs, lastID := in.SinceWithLastID(0); len(msgs) != 0 || lastID != 0 || msgs == nil {
		t.Errorf("empty inbox: got %v, lastID %d; want empty non-nil slice, 0", msgs, lastID)
	}

	a := in.Add(Message{Type: "dm", Text: "a"})
	b := in.Add(Message{Type: "channel", Text: "b"})

	msgs, lastID := in.SinceWithLastID(0)
	if len(msgs) != 2 || lastID != b.ID {
		t.Errorf("full read: got %d messages, lastID %d; want 2, %d", len(msgs), lastID, b.ID)
	}

	msgs, lastID = in.SinceWithLastID(a.ID)
	if len(msgs) != 1 || msgs[0].Text != "b" || lastID != b.ID {
		t.Errorf("after %d: got %v, lastID %d; want only %q, %d", a.ID, msgs, lastID, "b", b.ID)
	}

	// The pair must be consistent: lastID never outruns the returned
	// messages (the Since+LastID two-step race this method exists for).
	msgs, lastID = in.SinceWithLastID(b.ID)
	if len(msgs) != 0 || lastID != b.ID {
		t.Errorf("after newest: got %d messages, lastID %d; want 0, %d", len(msgs), lastID, b.ID)
	}

	// Since must keep behaving through the shared implementation (and stay
	// non-destructive and non-nil).
	if got := in.Since(a.ID); len(got) != 1 || got[0].Text != "b" {
		t.Errorf("Since after %d: got %v, want only %q", a.ID, got, "b")
	}
	if got := in.Since(0); got == nil || len(got) != 2 {
		t.Errorf("Since(0) = %v, want the same two messages", got)
	}
}
