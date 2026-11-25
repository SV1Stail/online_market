package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/SV1Stail/online_market/internal/observability"
)

type TelegramSender struct {
	client *http.Client
	url    string
	chatID string
	logger *slog.Logger
}

func NewTelegramSender(botToken, chatID string, logger *slog.Logger) *TelegramSender {
	return &TelegramSender{
		client: &http.Client{Timeout: 5 * time.Second},
		url:    "https://api.telegram.org/bot" + botToken + "/sendMessage",
		chatID: chatID,
		logger: logger,
	}
}

func (s *TelegramSender) Send(ctx context.Context, rawPayload json.RawMessage) error {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil || payload.Text == "" {
		safeErr := fmt.Errorf("invalid telegram notification payload")
		observability.LogError(ctx, s.logger, "telegram.decode_payload", safeErr)
		return safeErr
	}
	body, err := json.Marshal(map[string]string{"chat_id": s.chatID, "text": payload.Text})
	if err != nil {
		safeErr := fmt.Errorf("encode telegram request")
		observability.LogError(ctx, s.logger, "telegram.encode_request", safeErr)
		return safeErr
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		safeErr := fmt.Errorf("create telegram request")
		observability.LogError(ctx, s.logger, "telegram.create_request", safeErr)
		return safeErr
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		// The underlying URL contains the bot token, so do not wrap this error.
		safeErr := fmt.Errorf("telegram request failed")
		observability.LogError(ctx, s.logger, "telegram.send", safeErr, "error_type", fmt.Sprintf("%T", err))
		return safeErr
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		safeErr := fmt.Errorf("telegram returned HTTP %d", response.StatusCode)
		observability.LogError(ctx, s.logger, "telegram.response", safeErr, "http_status", response.StatusCode)
		return safeErr
	}
	return nil
}
