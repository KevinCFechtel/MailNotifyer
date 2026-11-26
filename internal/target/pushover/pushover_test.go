package pushover

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/KevinCFechtel/MailNotifyer/internal/delivery"
	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

func TestDeliverSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["token"] != "token" || payload["user"] != "user" {
			t.Errorf("unexpected credentials in payload")
		}
		if _, ok := payload["encrypted"]; ok {
			t.Error("unencrypted request contains encrypted flag")
		}
		if payload["title"] != "title" || payload["message"] != "message" {
			t.Error("unencrypted request does not contain plaintext fields")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":1,"request":"request-id"}`))
	}))
	defer server.Close()

	client, err := New("token", "user", "", server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = client.Deliver(context.Background(), notification.Notification{Title: "title", Message: "message"})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
}

func TestDeliverClassifiesFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		permanent bool
	}{
		{name: "bad request", status: http.StatusBadRequest, permanent: true},
		{name: "quota exceeded", status: http.StatusTooManyRequests, permanent: true},
		{name: "server error", status: http.StatusBadGateway, permanent: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"status":0,"request":"request-id","errors":["failed"]}`))
			}))
			defer server.Close()

			client, err := New("token", "user", "", server.URL, "", time.Second)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			err = client.Deliver(context.Background(), notification.Notification{Title: "title", Message: "message"})
			if err == nil {
				t.Fatal("Deliver() error = nil")
			}
			if got := delivery.IsPermanent(err); got != test.permanent {
				t.Errorf("IsPermanent() = %v, want %v", got, test.permanent)
			}
		})
	}
}

func TestDeliverEnforcesPushoverUnicodeLimits(t *testing.T) {
	t.Parallel()
	var title, message string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		title = payload["title"]
		message = payload["message"]
		_, _ = w.Write([]byte(`{"status":1,"request":"request-id"}`))
	}))
	defer server.Close()

	client, err := New("token", "user", "", server.URL, "", time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = client.Deliver(context.Background(), notification.Notification{
		Title:   strings.Repeat("🚨", 300),
		Message: strings.Repeat("🔥", 1200),
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if got := utf8.RuneCountInString(title); got != MaxTitleRunes {
		t.Errorf("title rune count = %d, want %d", got, MaxTitleRunes)
	}
	if got := utf8.RuneCountInString(message); got != MaxMessageRunes {
		t.Errorf("message rune count = %d, want %d", got, MaxMessageRunes)
	}
	if !strings.HasSuffix(title, "…") || !strings.HasSuffix(message, "…") {
		t.Errorf("truncated values do not end with ellipsis")
	}
}

func TestDeliverEncrypted(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString(testEncryptionKey)
	payloads := make(chan request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var payload request
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if strings.Contains(string(raw), testEncryptionKey) {
			t.Error("request exposes encryption key")
		}
		payloads <- payload
		_, _ = w.Write([]byte(`{"status":1,"request":"request-id"}`))
	}))
	defer server.Close()

	client, err := New("token", "user", "phone", server.URL, testEncryptionKey, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	item := notification.Notification{Title: "Geheimer Titel 🚨", Message: "From: root@example.test\n\nVertrauliche Nachricht"}
	var previous request
	for range 2 {
		if err := client.Deliver(context.Background(), item); err != nil {
			t.Fatalf("Deliver() error = %v", err)
		}
		payload := <-payloads
		if payload.Encrypted != 1 {
			t.Errorf("Encrypted = %d, want 1", payload.Encrypted)
		}
		if payload.Token != "token" || payload.User != "user" || payload.Device != "phone" {
			t.Error("encryption changed credentials or routing")
		}
		for _, field := range []struct {
			encrypted string
			want      string
		}{
			{encrypted: payload.Title, want: item.Title},
			{encrypted: payload.Message, want: item.Message},
		} {
			got, err := decryptTestField(field.encrypted, key)
			if err != nil || got != field.want {
				t.Errorf("decrypted field = %q, error = %v, want %q", got, err, field.want)
			}
		}
		if payload.Title == payload.Message || payload.Title == previous.Title || payload.Message == previous.Message {
			t.Error("fields or delivery attempts reused ciphertext")
		}
		previous = payload
	}
}

func TestDeliverEncryptionFailureDoesNotSend(t *testing.T) {
	t.Parallel()
	var contacted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		contacted.Store(true)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	defer server.Close()
	client, err := New("token", "user", "", server.URL, testEncryptionKey, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	// Simulate an invalid internal encryption state after successful startup.
	client.encryptionKey = []byte("invalid")
	err = client.Deliver(context.Background(), notification.Notification{Title: "secret", Message: "secret"})
	if err == nil || !delivery.IsPermanent(err) {
		t.Fatalf("Deliver() error = %v, want permanent encryption error", err)
	}
	if contacted.Load() {
		t.Error("encryption failure sent an HTTP request")
	}
}

func TestDeliverEncryptedEnforcesLimits(t *testing.T) {
	t.Parallel()
	key, _ := hex.DecodeString(testEncryptionKey)
	payloads := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload request
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		payloads <- payload
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	defer server.Close()
	client, err := New("token", "user", "", server.URL, testEncryptionKey, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	item := notification.Notification{
		Title:   strings.Repeat("🚨", 300),
		Message: strings.Repeat("🔥", 1200),
	}
	if err := client.Deliver(context.Background(), item); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	payload := <-payloads
	for _, field := range []struct {
		encrypted string
		original  string
		limit     int
	}{
		{encrypted: payload.Title, original: item.Title, limit: MaxTitleRunes},
		{encrypted: payload.Message, original: item.Message, limit: MaxMessageRunes},
	} {
		if len(field.encrypted) > field.limit {
			t.Errorf("encrypted length = %d, want <= %d", len(field.encrypted), field.limit)
		}
		got, err := decryptTestField(field.encrypted, key)
		if err != nil {
			t.Fatalf("decryptTestField() error = %v", err)
		}
		// Highly compressible text fits without any additional truncation.
		if want := notification.Truncate(field.original, field.limit); got != want {
			t.Errorf("decrypted field = %q, want %q", got, want)
		}
	}
}
