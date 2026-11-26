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

	got, err := Parse(strings.NewReader(raw), "envelope@example.test", []string{"alerts@example.test"}, now, Options{})
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

	got, err := Parse(strings.NewReader(raw), "", []string{"alerts@example.test"}, time.Now(), Options{})
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
			got, err := Parse(strings.NewReader(raw), test.envelopeFrom, []string{"alerts@example.test"}, time.Now(), Options{})
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

func TestParseUnattendedUpgrades(t *testing.T) {
	t.Parallel()
	const summary = "Packages upgraded: openssl"
	const packageLog = summary + "\n\nPackage installation log:\nPackage details"
	const fullBody = packageLog + "\n\nUnattended-upgrades log:\nUpgrade details"

	tests := []struct {
		name        string
		mode        UnattendedUpgradesTrimFrom
		subject     string
		body        string
		contentType string
		want        string
	}{
		{name: "default", body: fullBody, want: summary},
		{name: "package log", mode: TrimPackageInstallationLog, body: fullBody, want: summary},
		{name: "unattended log", mode: TrimUnattendedUpgradesLog, body: fullBody, want: packageLog},
		{name: "disabled", mode: TrimNone, body: fullBody, want: fullBody},
		{name: "CRLF", body: strings.ReplaceAll(fullBody, "\n", "\r\n"), want: summary},
		{name: "heading whitespace", body: summary + "\n \tPackage installation log: \t\nDetails", want: summary},
		{name: "heading at end", body: summary + "\nPackage installation log:", want: summary},
		{name: "heading at start", body: "Package installation log:\nDetails", want: "(No message body)"},
		{name: "missing package heading", body: summary + "\nUnattended-upgrades log:\nDetails", want: summary + "\nUnattended-upgrades log:\nDetails"},
		{name: "missing unattended heading", mode: TrimUnattendedUpgradesLog, body: packageLog, want: packageLog},
		{name: "no logs", body: summary, want: summary},
		{name: "inline mention", body: "See Package installation log: below\nDetails", want: "See Package installation log: below\nDetails"},
		{name: "heading prefix only", body: "Package installation log: not a heading\nDetails", want: "Package installation log: not a heading\nDetails"},
		{name: "other mail", subject: "Regular monitoring report", body: fullBody, want: fullBody},
		{name: "case insensitive subject", subject: "UNATTENDED-UPGRADES result for host: SUCCESS", body: fullBody, want: summary},
		{name: "encoded subject", subject: "=?UTF-8?Q?unattended-upgrades_result_for_host?=", body: fullBody, want: summary},
		{name: "HTML fallback", body: "<p>" + summary + "</p><p>Package installation log:</p><p>Details</p>", contentType: "text/html", want: summary},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			subject := test.subject
			if subject == "" {
				subject = "unattended-upgrades result for host: SUCCESS"
			}
			contentType := test.contentType
			if contentType == "" {
				contentType = "text/plain"
			}
			raw := "From: root@example.test\r\nSubject: " + subject +
				"\r\nContent-Type: " + contentType + "; charset=utf-8\r\n\r\n" + test.body
			got, err := Parse(strings.NewReader(raw), "", nil, time.Now(), Options{
				UnattendedUpgradesTrimFrom: test.mode,
			})
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if want := "From: root@example.test\n\n" + test.want; got.Message != want {
				t.Errorf("Message = %q, want %q", got.Message, want)
			}
		})
	}
}
