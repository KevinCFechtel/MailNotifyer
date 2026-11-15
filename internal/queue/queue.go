package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KevinCFechtel/MailNotifyer/internal/delivery"
	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

type Item struct {
	Notification notification.Notification `json:"notification"`
	Attempts     int                       `json:"attempts"`
	NextAttempt  time.Time                 `json:"next_attempt"`
	LastError    string                    `json:"last_error,omitempty"`
}

type Queue struct {
	dir    string
	target delivery.Target
	logger *slog.Logger
	wake   chan struct{}
	now    func() time.Time
}

func New(dir string, target delivery.Target, logger *slog.Logger) (*Queue, error) {
	if dir == "" {
		return nil, errors.New("queue directory is required")
	}
	if err := os.MkdirAll(filepath.Join(dir, "pending"), 0o750); err != nil {
		return nil, fmt.Errorf("create pending queue: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "failed"), 0o750); err != nil {
		return nil, fmt.Errorf("create failed queue: %w", err)
	}
	return &Queue{
		dir:    dir,
		target: target,
		logger: logger,
		wake:   make(chan struct{}, 1),
		now:    time.Now,
	}, nil
}

// Enqueue durably stores a notification before returning.
func (q *Queue) Enqueue(value notification.Notification) error {
	item := Item{
		Notification: value,
		NextAttempt:  q.now().UTC(),
	}
	path := filepath.Join(q.dir, "pending", value.ID+".json")
	if err := writeAtomic(path, item); err != nil {
		return fmt.Errorf("persist queue item: %w", err)
	}
	q.signal()
	return nil
}

func (q *Queue) Run(ctx context.Context) {
	q.logger.Info("delivery worker started", "target", q.target.Name())
	for {
		q.processReady(ctx)

		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			q.logger.Info("delivery worker stopped")
			return
		case <-q.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (q *Queue) processReady(ctx context.Context) {
	entries, err := os.ReadDir(filepath.Join(q.dir, "pending"))
	if err != nil {
		q.logger.Error("cannot read queue", "error", err)
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(q.dir, "pending", entry.Name())
		item, err := readItem(path)
		if err != nil {
			q.logger.Error("cannot read queue item", "file", entry.Name(), "error", err)
			q.moveFailed(path, entry.Name(), "invalid queue item: "+err.Error())
			continue
		}
		if q.now().Before(item.NextAttempt) {
			continue
		}

		err = q.target.Deliver(ctx, item.Notification)
		if err == nil {
			if removeErr := removeDurable(path); removeErr != nil {
				q.logger.Error("notification delivered but queue item cannot be removed",
					"id", item.Notification.ID, "error", removeErr)
				continue
			}
			q.logger.Info("notification delivered",
				"id", item.Notification.ID, "target", q.target.Name(), "attempts", item.Attempts+1)
			continue
		}
		if ctx.Err() != nil {
			return
		}

		item.Attempts++
		item.LastError = err.Error()
		if delivery.IsPermanent(err) {
			if writeErr := writeAtomic(path, item); writeErr != nil {
				q.logger.Error("cannot record permanent delivery failure",
					"id", item.Notification.ID, "error", writeErr)
				continue
			}
			q.moveFailed(path, entry.Name(), err.Error())
			continue
		}

		item.NextAttempt = q.now().Add(backoff(item.Attempts)).UTC()
		if writeErr := writeAtomic(path, item); writeErr != nil {
			q.logger.Error("cannot update queue item after delivery failure",
				"id", item.Notification.ID, "error", writeErr)
			continue
		}
		q.logger.Warn("temporary delivery failure",
			"id", item.Notification.ID,
			"target", q.target.Name(),
			"attempts", item.Attempts,
			"next_attempt", item.NextAttempt,
			"error", err)
	}
}

func (q *Queue) moveFailed(path, name, reason string) {
	destination := filepath.Join(q.dir, "failed", name)
	if err := os.Rename(path, destination); err != nil {
		q.logger.Error("cannot move queue item to failed directory", "file", name, "error", err)
		return
	}
	if err := syncDirs(filepath.Dir(path), filepath.Dir(destination)); err != nil {
		q.logger.Error("cannot sync failed queue move", "file", name, "error", err)
		return
	}
	q.logger.Error("notification permanently failed", "file", name, "reason", reason)
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func readItem(path string) (Item, error) {
	file, err := os.Open(path)
	if err != nil {
		return Item{}, err
	}
	defer file.Close()
	var item Item
	if err := json.NewDecoder(file).Decode(&item); err != nil {
		return Item{}, err
	}
	return item, nil
}

func writeAtomic(path string, value any) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".queue-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)

	encoder := json.NewEncoder(temp)
	if err := encoder.Encode(value); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func removeDurable(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDirs(dirs ...string) error {
	seen := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		if err := syncDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func syncDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func backoff(attempt int) time.Duration {
	seconds := 5 * math.Pow(2, float64(max(attempt-1, 0)))
	return min(time.Duration(seconds)*time.Second, time.Hour)
}
