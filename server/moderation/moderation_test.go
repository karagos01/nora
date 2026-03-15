package moderation

import (
	"testing"
	"time"
)

func TestCheckContent_CaseInsensitive(t *testing.T) {
	am := New()
	am.SetWordFilter([]string{"badword", "SPAM"})

	tests := []struct {
		content string
		blocked bool
		matched string
	}{
		{"hello world", false, ""},
		{"this has badword in it", true, "badword"},
		{"this has BADWORD in it", true, "badword"},
		{"this has BadWord in it", true, "badword"},
		{"SPAM message", true, "SPAM"},
		{"spam message", true, "SPAM"},
		{"nothing wrong here", false, ""},
	}

	for _, tt := range tests {
		blocked, matched := am.CheckContent(tt.content)
		if blocked != tt.blocked || matched != tt.matched {
			t.Errorf("CheckContent(%q) = (%v, %q), want (%v, %q)", tt.content, blocked, matched, tt.blocked, tt.matched)
		}
	}
}

func TestCheckContent_Phrases(t *testing.T) {
	am := New()
	am.SetWordFilter([]string{"bad phrase"})

	blocked, _ := am.CheckContent("this is a bad phrase here")
	if !blocked {
		t.Error("expected phrase to be blocked")
	}

	blocked, _ = am.CheckContent("bad and phrase separately")
	if blocked {
		t.Error("should not block words that appear separately")
	}
}

func TestCheckContent_EmptyFilter(t *testing.T) {
	am := New()
	am.SetWordFilter([]string{})

	blocked, _ := am.CheckContent("anything goes")
	if blocked {
		t.Error("empty filter should not block anything")
	}
}

func TestCheckContent_EmptyEntry(t *testing.T) {
	am := New()
	am.SetWordFilter([]string{"", "real"})

	// Prázdný řetězec by neměl matchnout vše
	blocked, matched := am.CheckContent("test")
	if blocked {
		t.Errorf("empty filter entry should not match, got matched=%q", matched)
	}

	blocked, _ = am.CheckContent("this is real bad")
	if !blocked {
		t.Error("expected 'real' to be blocked")
	}
}

func TestTrackMessage_UnderLimit(t *testing.T) {
	am := New()
	am.SpamMaxMessages = 3
	am.SpamWindowSeconds = 10

	for i := 0; i < 3; i++ {
		if am.TrackMessage("user1") {
			t.Errorf("message %d should not be spam (limit is 3)", i+1)
		}
	}
}

func TestTrackMessage_OverLimit(t *testing.T) {
	am := New()
	am.SpamMaxMessages = 3
	am.SpamWindowSeconds = 10

	for i := 0; i < 3; i++ {
		am.TrackMessage("user1")
	}

	if !am.TrackMessage("user1") {
		t.Error("4th message should be spam (limit is 3)")
	}
}

func TestTrackMessage_WindowExpiry(t *testing.T) {
	am := New()
	am.SpamMaxMessages = 2
	am.SpamWindowSeconds = 1

	am.TrackMessage("user1")
	am.TrackMessage("user1")

	// Počkat až okno vyprší
	time.Sleep(1100 * time.Millisecond)

	if am.TrackMessage("user1") {
		t.Error("message after window expiry should not be spam")
	}
}

func TestTrackMessage_SeparateUsers(t *testing.T) {
	am := New()
	am.SpamMaxMessages = 2
	am.SpamWindowSeconds = 10

	am.TrackMessage("user1")
	am.TrackMessage("user1")

	// user2 by neměl být ovlivněn
	if am.TrackMessage("user2") {
		t.Error("user2 should not be affected by user1's messages")
	}
}

func TestSetWordFilter_UpdatesCache(t *testing.T) {
	am := New()
	am.SetWordFilter([]string{"First"})

	blocked, _ := am.CheckContent("first word")
	if !blocked {
		t.Error("expected 'first' to be blocked")
	}

	am.SetWordFilter([]string{"Second"})

	blocked, _ = am.CheckContent("first word")
	if blocked {
		t.Error("'first' should no longer be blocked after filter update")
	}

	blocked, _ = am.CheckContent("second word")
	if !blocked {
		t.Error("expected 'second' to be blocked")
	}
}
