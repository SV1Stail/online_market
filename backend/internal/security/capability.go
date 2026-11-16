package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

type CapabilityTokens struct {
	secret []byte
}

func NewCapabilityTokens(secret string) *CapabilityTokens {
	return &CapabilityTokens{secret: []byte(secret)}
}

func (c *CapabilityTokens) Derive(purpose string) string {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(purpose))
	return hex.EncodeToString(mac.Sum(nil))
}
