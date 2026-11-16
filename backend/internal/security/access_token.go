package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid access token")

type Claims struct {
	UserID    string   `json:"sub"`
	Roles     []string `json:"roles"`
	ExpiresAt int64    `json:"exp"`
}

type Signer struct {
	secret []byte
}

func NewSigner(secret string) (*Signer, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("access token secret must contain at least 32 bytes")
	}
	return &Signer{secret: []byte(secret)}, nil
}

func (s *Signer) Sign(userID string, roles []string, expiresAt time.Time) (string, error) {
	claims := Claims{UserID: userID, Roles: roles, ExpiresAt: expiresAt.Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal access token: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := s.signature(encodedPayload)
	return encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *Signer) Verify(token string, now time.Time) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidToken
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(providedSignature, s.signature(parts[0])) {
		return nil, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrInvalidToken
	}
	if claims.UserID == "" || claims.ExpiresAt <= now.Unix() {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}

func (s *Signer) signature(payload string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}
