package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
)

type Repository interface {
	GetByLogin(context.Context, string) (*User, error)
	CreateSession(context.Context, Session) error
	RotateSession(context.Context, []byte, []byte, time.Time, time.Time) (*User, error)
	RevokeSession(context.Context, []byte, time.Time) error
	CreateUser(context.Context, User) error
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	UUID() (string, error)
	Token(int) (string, error)
}

type AccessSigner interface {
	Sign(string, []string, time.Time) (string, error)
}

type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
	signer     AccessSigner
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewService(repository Repository, clock Clock, ids IDGenerator, signer AccessSigner, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{
		repository: repository,
		clock:      clock,
		ids:        ids,
		signer:     signer,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *Service) Login(ctx context.Context, login, password string) (*Tokens, error) {
	login = strings.TrimSpace(login)
	if login == "" || len(login) > 255 || password == "" || len(password) > 1024 {
		return nil, apperror.ErrUnauthenticated
	}
	user, err := s.repository.GetByLogin(ctx, login)
	if errors.Is(err, apperror.ErrNotFound) {
		return nil, apperror.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("get admin user: %w", err)
	}
	if !user.Active || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, apperror.ErrUnauthenticated
	}
	return s.issue(ctx, *user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	if !identifier.IsToken(refreshToken, 32) {
		return nil, apperror.ErrUnauthenticated
	}
	newToken, err := s.ids.Token(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	now := s.clock.Now()
	refreshExpiresAt := now.Add(s.refreshTTL)
	user, err := s.repository.RotateSession(
		ctx,
		identifier.HashToken(refreshToken),
		identifier.HashToken(newToken),
		refreshExpiresAt,
		now,
	)
	if errors.Is(err, apperror.ErrUnauthenticated) || errors.Is(err, apperror.ErrNotFound) {
		return nil, apperror.ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("rotate admin session: %w", err)
	}
	if !user.Active {
		return nil, apperror.ErrUnauthenticated
	}
	accessExpiresAt := now.Add(s.accessTTL)
	accessToken, err := s.signer.Sign(user.ID, roleStrings(user.Roles), accessExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}
	return &Tokens{
		User:                  *user,
		AccessToken:           accessToken,
		RefreshToken:          newToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if !identifier.IsToken(refreshToken, 32) {
		return apperror.ErrUnauthenticated
	}
	if err := s.repository.RevokeSession(ctx, identifier.HashToken(refreshToken), s.clock.Now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) issue(ctx context.Context, user User) (*Tokens, error) {
	now := s.clock.Now()
	accessExpiresAt := now.Add(s.accessTTL)
	refreshExpiresAt := now.Add(s.refreshTTL)
	accessToken, err := s.signer.Sign(user.ID, roleStrings(user.Roles), accessExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}
	refreshToken, err := s.ids.Token(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	sessionID, err := s.ids.UUID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	if err := s.repository.CreateSession(ctx, Session{
		ID:               sessionID,
		UserID:           user.ID,
		RefreshTokenHash: identifier.HashToken(refreshToken),
		ExpiresAt:        refreshExpiresAt,
		CreatedAt:        now,
	}); err != nil {
		return nil, fmt.Errorf("create admin session: %w", err)
	}
	return &Tokens{
		User:                  user,
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

func roleStrings(roles []Role) []string {
	result := make([]string, len(roles))
	for index, role := range roles {
		result[index] = string(role)
	}
	return result
}
