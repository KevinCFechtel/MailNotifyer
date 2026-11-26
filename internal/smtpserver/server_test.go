package smtpserver

import (
	"io"
	"log/slog"
	"net"
	netsmtp "net/smtp"
	"strings"
	"testing"
	"time"

	smtp "github.com/emersion/go-smtp"

	"github.com/KevinCFechtel/MailNotifyer/internal/mailparser"
	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

type memoryQueue struct {
	items []notification.Notification
}

func (q *memoryQueue) Enqueue(value notification.Notification) error {
	q.items = append(q.items, value)
	return nil
}

func TestAllowlist(t *testing.T) {
	t.Parallel()
	backend, err := NewBackend(
		[]string{"10.0.0.0/8", "2001:db8::/32"},
		&memoryQueue{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		mailparser.Options{},
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	tests := []struct {
		ip      string
		allowed bool
	}{
		{ip: "10.1.2.3", allowed: true},
		{ip: "192.168.1.1", allowed: false},
		{ip: "2001:db8::1", allowed: true},
		{ip: "2001:db9::1", allowed: false},
	}
	for _, test := range tests {
		if got := backend.isAllowed(net.ParseIP(test.ip)); got != test.allowed {
			t.Errorf("isAllowed(%s) = %v, want %v", test.ip, got, test.allowed)
		}
	}
}

func TestInvalidAllowlist(t *testing.T) {
	t.Parallel()
	_, err := NewBackend(
		[]string{"not-a-cidr"},
		&memoryQueue{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		mailparser.Options{},
	)
	if err == nil {
		t.Fatal("NewBackend() error = nil")
	}
}

func TestSMTPAcceptanceQueuesMail(t *testing.T) {
	queue := &memoryQueue{}
	backend, err := NewBackend(
		[]string{"127.0.0.0/8"},
		queue,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		mailparser.Options{},
	)
	if err != nil {
		t.Fatalf("NewBackend() error = %v", err)
	}
	server := smtp.NewServer(backend)
	server.Domain = "mailnotifier.test"
	server.ReadTimeout = 5 * time.Second
	server.WriteTimeout = 5 * time.Second
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		<-done
	})

	raw := strings.Join([]string{
		"From: Monitoring <monitoring@example.test>",
		"To: alerts@example.test",
		"Subject: Integration test",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Everything is fine.",
	}, "\r\n")
	err = netsmtp.SendMail(
		listener.Addr().String(),
		nil,
		"monitoring@example.test",
		[]string{"alerts@example.test"},
		[]byte(raw),
	)
	if err != nil {
		t.Fatalf("SendMail() error = %v", err)
	}
	if len(queue.items) != 1 {
		t.Fatalf("queued count = %d, want 1", len(queue.items))
	}
	if queue.items[0].Title != "Integration test" {
		t.Errorf("queued title = %q", queue.items[0].Title)
	}
}

func TestDataQueuesTrimmedUnattendedUpgrades(t *testing.T) {
	t.Parallel()
	const summary = "Packages upgraded: openssl"
	const packageLog = summary + "\n\nPackage installation log:\nPackage details"
	const fullBody = packageLog + "\n\nUnattended-upgrades log:\nUpgrade details"
	tests := []struct {
		mode mailparser.UnattendedUpgradesTrimFrom
		want string
	}{
		{mode: mailparser.TrimPackageInstallationLog, want: summary},
		{mode: mailparser.TrimUnattendedUpgradesLog, want: packageLog},
		{mode: mailparser.TrimNone, want: fullBody},
	}
	for _, test := range tests {
		t.Run(string(test.mode), func(t *testing.T) {
			t.Parallel()
			queue := &memoryQueue{}
			backend, err := NewBackend(
				[]string{"127.0.0.0/8"},
				queue,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				mailparser.Options{UnattendedUpgradesTrimFrom: test.mode},
			)
			if err != nil {
				t.Fatalf("NewBackend() error = %v", err)
			}
			session := &session{backend: backend, from: "root@example.test", recipients: []string{"alerts@example.test"}}
			raw := "Subject: unattended-upgrades result for host: SUCCESS\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n\r\n" + fullBody
			if err := session.Data(strings.NewReader(raw)); err != nil {
				t.Fatalf("Data() error = %v", err)
			}
			if len(queue.items) != 1 {
				t.Fatalf("queued count = %d, want 1", len(queue.items))
			}
			if want := "From: root@example.test\n\n" + test.want; queue.items[0].Message != want {
				t.Errorf("queued message = %q, want %q", queue.items[0].Message, want)
			}
		})
	}
}
