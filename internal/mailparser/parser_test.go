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
		"Subject: =?UTF-8?Q?Check_successful_=F0=9F=9A=A8?=",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Everything is fine.",
	}, "\r\n")
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	got, err := Parse(strings.NewReader(raw), "envelope@example.test", []string{"alerts@example.test"}, now)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Title != "Check successful 🚨" {
		t.Errorf("Title = %q", got.Title)
	}
	if !strings.Contains(got.Message, "From: Müller <sender@example.test>") {
		t.Errorf("Message does not contain decoded sender: %q", got.Message)
	}
	if !strings.Contains(got.Message, "Everything is fine.") {
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
		"<p>Alert: <strong>High temperature</strong></p>"

	got, err := Parse(strings.NewReader(raw), "", []string{"alerts@example.test"}, time.Now())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !strings.Contains(got.Message, "High temperature") {
		t.Errorf("Message does not contain HTML fallback text: %q", got.Message)
	}
}

func TestParseMissingFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		envelopeFrom string
		wantFrom     string
	}{
		{name: "unknown sender", wantFrom: "(unknown)"},
		{name: "envelope sender", envelopeFrom: "sender@example.test", wantFrom: "sender@example.test"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw := "To: alerts@example.test\r\n\r\n"
			got, err := Parse(strings.NewReader(raw), test.envelopeFrom, []string{"alerts@example.test"}, time.Now())
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got.From != test.wantFrom {
				t.Errorf("From = %q, want %q", got.From, test.wantFrom)
			}
			if got.Title != "(No subject)" {
				t.Errorf("Title = %q, want %q", got.Title, "(No subject)")
			}
			if want := "From: " + test.wantFrom + "\n\n(No message body)"; got.Message != want {
				t.Errorf("Message = %q, want %q", got.Message, want)
			}
		})
	}
}
