package pushover

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":1,"request":"request-id"}`))
	}))
	defer server.Close()

	client := New("token", "user", "", server.URL, time.Second)
	err := client.Deliver(context.Background(), notification.Notification{Title: "title", Message: "message"})
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

			client := New("token", "user", "", server.URL, time.Second)
			err := client.Deliver(context.Background(), notification.Notification{Title: "title", Message: "message"})
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

	client := New("token", "user", "", server.URL, time.Second)
	err := client.Deliver(context.Background(), notification.Notification{
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
