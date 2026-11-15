package mailparser

import (
	"strings"
	"testing"
	"time"
)

func TestParsePlainTextMail(t *testing.T) {
	t.Parallel()
	raw := strings.Join([]string{
		"From: =?UTF-8?Q?M=C3=BCller?= <sender@example.test>",
		"To: alerts@example.test",
		"Subject: =?UTF-8?Q?Pr=C3=BCfung_erfolgreich?=",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Alles in Ordnung.",
	}, "\r\n")
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	got, err := Parse(strings.NewReader(raw), "envelope@example.test", []string{"alerts@example.test"}, now)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Title != "Prüfung erfolgreich" {
		t.Errorf("Title = %q", got.Title)
	}
	if !strings.Contains(got.Message, "Von: Müller <sender@example.test>") {
		t.Errorf("Message does not contain decoded sender: %q", got.Message)
	}
	if !strings.Contains(got.Message, "Alles in Ordnung.") {
		t.Errorf("Message does not contain body: %q", got.Message)
	}
	if got.ReceivedAt != now {
		t.Errorf("ReceivedAt = %v, want %v", got.ReceivedAt, now)
	}
}

func TestParseHTMLFallback(t *testing.T) {
	t.Parallel()
	raw := "From: sender@example.test\r\n" +
		"Subject: HTML only\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Alarm: <strong>Temperatur hoch</strong></p>"

	got, err := Parse(strings.NewReader(raw), "", []string{"alerts@example.test"}, time.Now())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !strings.Contains(got.Message, "Temperatur hoch") {
		t.Errorf("Message does not contain HTML fallback text: %q", got.Message)
	}
}
