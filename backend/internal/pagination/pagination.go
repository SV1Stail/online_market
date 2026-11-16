package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SV1Stail/online_market/internal/identifier"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func PageSize(requested uint32) int {
	if requested == 0 {
		return defaultPageSize
	}
	if requested > maxPageSize {
		return maxPageSize
	}
	return int(requested)
}

func Encode(cursor Cursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal page cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func Decode(token string) (*Cursor, error) {
	if token == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decode page token: %w", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fmt.Errorf("unmarshal page token: %w", err)
	}
	if cursor.CreatedAt.IsZero() || !identifier.IsUUID(cursor.ID) {
		return nil, fmt.Errorf("page token is incomplete")
	}
	return &cursor, nil
}
