package queue

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KevinCFechtel/MailNotifyer/internal/delivery"
	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

type fakeTarget struct {
	err       error
	delivered []notification.Notification
}

func (t *fakeTarget) Name() string {
	return "fake"
}

func (t *fakeTarget) Deliver(_ context.Context, value notification.Notification) error {
	t.delivered = append(t.delivered, value)
	return t.err
}

func TestEnqueueAndDeliver(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{}
	q := newTestQueue(t, target)
	item := notification.Notification{ID: "test-id", Title: "title", Message: "message"}

	if err := q.Enqueue(item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	assertFileExists(t, filepath.Join(q.dir, "pending", "test-id.json"))

	q.processReady(context.Background())

	if len(target.delivered) != 1 {
		t.Fatalf("delivered count = %d, want 1", len(target.delivered))
	}
	assertFileMissing(t, filepath.Join(q.dir, "pending", "test-id.json"))
}

func TestPermanentFailureMovesItem(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{err: delivery.Permanent(errors.New("invalid credentials"))}
	q := newTestQueue(t, target)
	item := notification.Notification{ID: "permanent-id", Title: "title", Message: "message"}

	if err := q.Enqueue(item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	q.processReady(context.Background())

	assertFileMissing(t, filepath.Join(q.dir, "pending", "permanent-id.json"))
	failedPath := filepath.Join(q.dir, "failed", "permanent-id.json")
	assertFileExists(t, failedPath)
	failed, err := readItem(failedPath)
	if err != nil {
		t.Fatalf("readItem() error = %v", err)
	}
	if failed.Attempts != 1 || failed.LastError == "" {
		t.Errorf("failed item = %+v", failed)
	}
}

func TestTemporaryFailureSchedulesRetry(t *testing.T) {
	t.Parallel()
	target := &fakeTarget{err: delivery.Temporary(errors.New("network unavailable"))}
	q := newTestQueue(t, target)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return now }
	item := notification.Notification{ID: "temporary-id", Title: "title", Message: "message"}

	if err := q.Enqueue(item); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	q.processReady(context.Background())

	pending, err := readItem(filepath.Join(q.dir, "pending", "temporary-id.json"))
	if err != nil {
		t.Fatalf("readItem() error = %v", err)
	}
	if pending.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", pending.Attempts)
	}
	if want := now.Add(5 * time.Second); !pending.NextAttempt.Equal(want) {
		t.Errorf("NextAttempt = %v, want %v", pending.NextAttempt, want)
	}

	q.processReady(context.Background())
	if len(target.delivered) != 1 {
		t.Errorf("delivery was retried before NextAttempt")
	}
}

func newTestQueue(t *testing.T, target delivery.Target) *Queue {
	t.Helper()
	q, err := New(
		t.TempDir(),
		target,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return q
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing file %s, got %v", path, err)
	}
}
