package mailparser

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jhillyerd/enmime"

	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

// Parse converts an RFC 5322/MIME message into a target-independent notification.
func Parse(r io.Reader, envelopeFrom string, recipients []string, now time.Time) (notification.Notification, error) {
	env, err := enmime.ReadEnvelope(r)
	if err != nil {
		return notification.Notification{}, fmt.Errorf("parse MIME message: %w", err)
	}

	from := strings.TrimSpace(env.GetHeader("From"))
	if from == "" {
		from = strings.TrimSpace(envelopeFrom)
	}
	if from == "" {
		from = "(unbekannt)"
	}

	title := strings.TrimSpace(env.GetHeader("Subject"))
	if title == "" {
		title = "(Kein Betreff)"
	}

	body := strings.TrimSpace(env.Text)
	if body == "" {
		body = "(Kein Nachrichtentext)"
	}
	message := "Von: " + from + "\n\n" + body

	id, err := newID()
	if err != nil {
		return notification.Notification{}, fmt.Errorf("create notification ID: %w", err)
	}

	return notification.Notification{
		ID:         id,
		Title:      title,
		Message:    message,
		From:       from,
		Recipients: append([]string(nil), recipients...),
		ReceivedAt: now.UTC(),
	}, nil
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
