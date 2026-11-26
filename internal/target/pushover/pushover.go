package pushover

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KevinCFechtel/MailNotifyer/internal/delivery"
	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

const DefaultEndpoint = "https://api.pushover.net/1/messages.json"

const (
	MaxTitleRunes   = 250
	MaxMessageRunes = 1024
)

type Client struct {
	token         string
	user          string
	device        string
	endpoint      string
	httpClient    *http.Client
	encryptionKey []byte
}

type request struct {
	Token     string `json:"token"`
	User      string `json:"user"`
	Device    string `json:"device,omitempty"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Encrypted int    `json:"encrypted,omitempty"`
}

type response struct {
	Status  int      `json:"status"`
	Request string   `json:"request"`
	Errors  []string `json:"errors"`
}

func New(token, user, device, endpoint, encryptionKey string, timeout time.Duration) (*Client, error) {
	key, err := parseEncryptionKey(encryptionKey)
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		token:         token,
		user:          user,
		device:        device,
		endpoint:      endpoint,
		encryptionKey: key,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *Client) Name() string {
	return "pushover"
}

func (c *Client) Deliver(ctx context.Context, item notification.Notification) error {
	data := request{
		Token:   c.token,
		User:    c.user,
		Device:  c.device,
		Title:   notification.Truncate(item.Title, MaxTitleRunes),
		Message: notification.Truncate(item.Message, MaxMessageRunes),
	}
	if c.encryptionKey != nil {
		var err error
		data.Title, err = encryptField(data.Title, c.encryptionKey, MaxTitleRunes)
		if err != nil {
			return delivery.Permanent(fmt.Errorf("encrypt Pushover title: %w", err))
		}
		data.Message, err = encryptField(data.Message, c.encryptionKey, MaxMessageRunes)
		if err != nil {
			return delivery.Permanent(fmt.Errorf("encrypt Pushover message: %w", err))
		}
		data.Encrypted = 1
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return delivery.Permanent(fmt.Errorf("encode Pushover request: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return delivery.Permanent(fmt.Errorf("create Pushover request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "MailNotifyer/1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return delivery.Temporary(fmt.Errorf("send Pushover request: %w", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return delivery.Temporary(fmt.Errorf("read Pushover response: %w", err))
	}

	var result response
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode >= 500 {
			return delivery.Temporary(fmt.Errorf("Pushover returned HTTP %d with invalid JSON", resp.StatusCode))
		}
		return delivery.Permanent(fmt.Errorf("Pushover returned HTTP %d with invalid JSON", resp.StatusCode))
	}

	if resp.StatusCode == http.StatusOK && result.Status == 1 {
		return nil
	}

	detail := strings.Join(result.Errors, "; ")
	if detail == "" {
		detail = http.StatusText(resp.StatusCode)
	}
	err = fmt.Errorf("Pushover rejected request %q with HTTP %d: %s", result.Request, resp.StatusCode, detail)
	if resp.StatusCode >= 500 {
		return delivery.Temporary(err)
	}
	// Pushover explicitly documents that retries of 4xx responses do not help.
	return delivery.Permanent(err)
}
