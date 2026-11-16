package security

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignerRoundTrip(t *testing.T) {
	signer, err := NewSigner("01234567890123456789012345678901")
	require.NoError(t, err)
	now := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)

	token, err := signer.Sign("user-id", []string{"admin"}, now.Add(time.Minute))
	require.NoError(t, err)
	claims, err := signer.Verify(token, now)
	require.NoError(t, err)
	require.Equal(t, "user-id", claims.UserID)
	require.Equal(t, []string{"admin"}, claims.Roles)
}

func TestSignerRejectsExpiredToken(t *testing.T) {
	signer, err := NewSigner("01234567890123456789012345678901")
	require.NoError(t, err)
	now := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)
	token, err := signer.Sign("user-id", []string{"admin"}, now)
	require.NoError(t, err)

	_, err = signer.Verify(token, now)
	require.ErrorIs(t, err, ErrInvalidToken)
}
