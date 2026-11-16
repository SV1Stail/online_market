package identifier

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type Generator struct{}

func (Generator) UUID() (string, error) {
	return NewUUID()
}

func (Generator) Token(byteLength int) (string, error) {
	return NewToken(byteLength)
}

func NewUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(value[0:4]),
		hex.EncodeToString(value[4:6]),
		hex.EncodeToString(value[6:8]),
		hex.EncodeToString(value[8:10]),
		hex.EncodeToString(value[10:16]),
	), nil
}

func NewToken(byteLength int) (string, error) {
	if byteLength < 16 {
		return "", fmt.Errorf("token length must be at least 16 bytes")
	}

	value := make([]byte, byteLength)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func HashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func IsUUID(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return false
	}
	for _, part := range parts {
		if _, err := hex.DecodeString(part); err != nil {
			return false
		}
	}
	return true
}

func IsToken(value string, byteLength int) bool {
	if len(value) != byteLength*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
