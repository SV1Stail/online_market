package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

type Message struct {
	ID       string
	OrderID  string
	Type     string
	Channel  string
	Payload  json.RawMessage
	Attempts int
}

type Sender interface {
	Send(context.Context, json.RawMessage) error
}

type Repository interface {
	Claim(context.Context, time.Time, time.Duration) (*Message, error)
	MarkSent(context.Context, string, time.Time) error
	MarkFailed(context.Context, string, int, int, time.Time, string) error
}

type Clock interface {
	Now() time.Time
}

type Worker struct {
	repository   Repository
	senders      map[string]Sender
	clock        Clock
	logger       *slog.Logger
	pollInterval time.Duration
	lockTimeout  time.Duration
	maxAttempts  int
}

func NewWorker(repository Repository, senders map[string]Sender, clock Clock, logger *slog.Logger, pollInterval time.Duration) *Worker {
	return &Worker{
		repository:   repository,
		senders:      senders,
		clock:        clock,
		logger:       logger,
		pollInterval: pollInterval,
		lockTimeout:  5 * time.Minute,
		maxAttempts:  8,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		processed, err := w.processOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("notification worker iteration failed", "error", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := w.clock.Now()
	message, err := w.repository.Claim(ctx, now, w.lockTimeout)
	if err != nil {
		return false, fmt.Errorf("claim notification: %w", err)
	}
	if message == nil {
		return false, nil
	}
	sender, exists := w.senders[message.Channel]
	if !exists {
		err := fmt.Errorf("notification channel is not configured")
		return true, w.fail(ctx, message, now, err)
	}
	if err := sender.Send(ctx, message.Payload); err != nil {
		return true, w.fail(ctx, message, now, err)
	}
	if err := w.repository.MarkSent(ctx, message.ID, now); err != nil {
		return true, fmt.Errorf("mark notification sent: %w", err)
	}
	w.logger.Info("notification sent", "notification_id", message.ID, "channel", message.Channel)
	return true, nil
}

func (w *Worker) fail(ctx context.Context, message *Message, now time.Time, sendErr error) error {
	delay := retryDelay(message.Attempts)
	if err := w.repository.MarkFailed(
		ctx,
		message.ID,
		message.Attempts,
		w.maxAttempts,
		now.Add(delay),
		safeError(sendErr),
	); err != nil {
		return fmt.Errorf("mark notification failed: %w", err)
	}
	w.logger.Warn(
		"notification delivery failed",
		"notification_id", message.ID,
		"channel", message.Channel,
		"attempt", message.Attempts,
		"error", safeError(sendErr),
	)
	return nil
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<uint(attempt-1)) * time.Minute
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}
