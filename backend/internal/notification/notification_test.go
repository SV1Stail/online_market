package notification

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkerProcessOneAddsFiveSecondDeadline(t *testing.T) {
	repository := &deadlineRepositoryStub{}
	worker := NewWorker(
		repository,
		nil,
		fixedClock{now: time.Now()},
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		time.Second,
	)
	startedAt := time.Now()

	processed, err := worker.processOne(context.Background())

	require.NoError(t, err)
	require.False(t, processed)
	require.True(t, repository.hasDeadline)
	require.WithinDuration(t, startedAt.Add(5*time.Second), repository.deadline, 250*time.Millisecond)
}

func TestTelegramSenderUsesFiveSecondHTTPTimeout(t *testing.T) {
	sender := NewTelegramSender("token", "chat", slog.New(slog.NewJSONHandler(io.Discard, nil)))

	require.Equal(t, 5*time.Second, sender.client.Timeout)
}

type deadlineRepositoryStub struct {
	deadline    time.Time
	hasDeadline bool
}

func (s *deadlineRepositoryStub) Claim(ctx context.Context, _ time.Time, _ time.Duration) (*Message, error) {
	s.deadline, s.hasDeadline = ctx.Deadline()
	return nil, nil
}

func (s *deadlineRepositoryStub) MarkSent(context.Context, string, time.Time) error {
	return nil
}

func (s *deadlineRepositoryStub) MarkFailed(context.Context, string, int, int, time.Time, string) error {
	return nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}
