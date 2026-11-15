package smtpserver

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	smtp "github.com/emersion/go-smtp"

	"github.com/KevinCFechtel/MailNotifyer/internal/mailparser"
	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

type Enqueuer interface {
	Enqueue(notification.Notification) error
}

type Backend struct {
	allowed []*net.IPNet
	queue   Enqueuer
	logger  *slog.Logger
	now     func() time.Time
}

func NewBackend(cidrs []string, queue Enqueuer, logger *slog.Logger) (*Backend, error) {
	allowed := make([]*net.IPNet, 0, len(cidrs))
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid SMTP allowlist CIDR %q: %w", value, err)
		}
		allowed = append(allowed, network)
	}
	if len(allowed) == 0 {
		return nil, errors.New("at least one SMTP allowlist CIDR is required")
	}
	return &Backend{allowed: allowed, queue: queue, logger: logger, now: time.Now}, nil
}

func (b *Backend) NewSession(conn *smtp.Conn) (smtp.Session, error) {
	host, _, err := net.SplitHostPort(conn.Conn().RemoteAddr().String())
	if err != nil {
		return nil, &smtp.SMTPError{Code: 421, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "Cannot determine client address"}
	}
	ip := net.ParseIP(host)
	if !b.isAllowed(ip) {
		b.logger.Warn("rejected SMTP client outside allowlist", "remote_ip", host)
		return nil, &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "Client not allowed"}
	}
	return &session{backend: b}, nil
}

func (b *Backend) isAllowed(ip net.IP) bool {
	for _, network := range b.allowed {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

type session struct {
	backend    *Backend
	from       string
	recipients []string
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	s.recipients = nil
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.recipients = append(s.recipients, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	item, err := mailparser.Parse(r, s.from, s.recipients, s.backend.now())
	if err != nil {
		s.backend.logger.Warn("rejected malformed message", "from", s.from, "error", err)
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 6, 0}, Message: "Invalid message content"}
	}
	if err := s.backend.queue.Enqueue(item); err != nil {
		s.backend.logger.Error("cannot queue message", "from", s.from, "error", err)
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "Temporary queue failure"}
	}
	s.backend.logger.Info("mail accepted and queued",
		"id", item.ID, "from", item.From, "recipients", item.Recipients)
	return nil
}

func (s *session) Reset() {
	s.from = ""
	s.recipients = nil
}

func (s *session) Logout() error {
	return nil
}
