package notification

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUsesUnicodeCodePoints(t *testing.T) {
	t.Parallel()
	got := Truncate(strings.Repeat("🚨", 10), 5)
	if utf8.RuneCountInString(got) != 5 {
		t.Errorf("rune count = %d, want 5", utf8.RuneCountInString(got))
	}
	if got != "🚨🚨🚨🚨…" {
		t.Errorf("Truncate() = %q", got)
	}
}
