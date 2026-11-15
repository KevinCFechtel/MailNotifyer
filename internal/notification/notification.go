package notification

import (
	"time"
	"unicode/utf8"
)

// Notification is the target-independent representation of an incoming mail.
type Notification struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Message    string    `json:"message"`
	From       string    `json:"from"`
	Recipients []string  `json:"recipients"`
	ReceivedAt time.Time `json:"received_at"`
}

// Truncate limits a string by Unicode code points and adds an ellipsis when needed.
func Truncate(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	if maxRunes == 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}
