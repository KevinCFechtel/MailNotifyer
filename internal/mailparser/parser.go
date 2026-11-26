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

type UnattendedUpgradesTrimFrom string

const (
	TrimPackageInstallationLog UnattendedUpgradesTrimFrom = "package-installation-log"
	TrimUnattendedUpgradesLog  UnattendedUpgradesTrimFrom = "unattended-upgrades-log"
	TrimNone                   UnattendedUpgradesTrimFrom = "none"
)

type Options struct {
	// The zero value trims from the package installation log.
	UnattendedUpgradesTrimFrom UnattendedUpgradesTrimFrom
}

// Parse converts an RFC 5322/MIME message into a target-independent notification.
func Parse(r io.Reader, envelopeFrom string, recipients []string, now time.Time, options Options) (notification.Notification, error) {
	env, err := enmime.ReadEnvelope(r)
	if err != nil {
		return notification.Notification{}, fmt.Errorf("parse MIME message: %w", err)
	}

	from := strings.TrimSpace(env.GetHeader("From"))
	if from == "" {
		from = strings.TrimSpace(envelopeFrom)
	}
	if from == "" {
		from = "(unknown)"
	}

	title := strings.TrimSpace(env.GetHeader("Subject"))
	if title == "" {
		title = "(No subject)"
	}

	body := strings.TrimSpace(trimUnattendedUpgradesLog(title, env.Text, options.UnattendedUpgradesTrimFrom))
	if body == "" {
		body = "(No message body)"
	}
	message := "From: " + from + "\n\n" + body

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

func trimUnattendedUpgradesLog(subject, body string, trimFrom UnattendedUpgradesTrimFrom) string {
	if !strings.Contains(strings.ToLower(subject), "unattended-upgrades") {
		return body
	}

	var marker string
	switch trimFrom {
	case "", TrimPackageInstallationLog:
		marker = "Package installation log:"
	case TrimUnattendedUpgradesLog:
		marker = "Unattended-upgrades log:"
	default:
		return body
	}

	offset := 0
	for line := range strings.SplitSeq(body, "\n") {
		if strings.TrimSpace(line) == marker {
			return body[:offset]
		}
		offset += len(line) + 1
	}
	return body
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
