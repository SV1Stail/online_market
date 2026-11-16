package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/SV1Stail/online_market/internal/auth"
	"github.com/SV1Stail/online_market/internal/identifier"
	platformpostgres "github.com/SV1Stail/online_market/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("create administrator failed", "error", err)
		os.Exit(1)
	}
	logger.Info("administrator created")
}

func run(logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	login := strings.TrimSpace(os.Getenv("ADMIN_LOGIN"))
	password := os.Getenv("ADMIN_PASSWORD")
	displayName := strings.TrimSpace(os.Getenv("ADMIN_DISPLAY_NAME"))
	roles, err := parseRoles(os.Getenv("ADMIN_ROLES"))
	if err != nil {
		return err
	}
	if databaseURL == "" || login == "" || displayName == "" {
		return fmt.Errorf("DATABASE_URL, ADMIN_LOGIN and ADMIN_DISPLAY_NAME are required")
	}
	if len(password) < 12 {
		return fmt.Errorf("ADMIN_PASSWORD must contain at least 12 characters")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	userID, err := identifier.NewUUID()
	if err != nil {
		return fmt.Errorf("generate admin id: %w", err)
	}
	pool, err := platformpostgres.Open(ctx, databaseURL, logger)
	if err != nil {
		return err
	}
	defer pool.Close()
	repository := auth.NewPostgresRepository(pool)
	return repository.CreateUser(ctx, auth.User{
		ID:           userID,
		Login:        login,
		PasswordHash: string(passwordHash),
		DisplayName:  displayName,
		Roles:        roles,
		Active:       true,
	})
}

func parseRoles(raw string) ([]auth.Role, error) {
	if strings.TrimSpace(raw) == "" {
		return []auth.Role{auth.RoleAdmin}, nil
	}
	roles := make([]auth.Role, 0)
	seen := make(map[auth.Role]struct{})
	for _, item := range strings.Split(raw, ",") {
		role := auth.Role(strings.ToLower(strings.TrimSpace(item)))
		if role != auth.RoleAdmin && role != auth.RoleManager {
			return nil, fmt.Errorf("unsupported admin role %q", item)
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	return roles, nil
}
