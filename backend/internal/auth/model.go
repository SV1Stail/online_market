package auth

import "time"

type Role string

const (
	RoleManager Role = "manager"
	RoleAdmin   Role = "admin"
)

type User struct {
	ID           string
	Login        string
	PasswordHash string
	DisplayName  string
	Roles        []Role
	Active       bool
}

type Session struct {
	ID               string
	UserID           string
	RefreshTokenHash []byte
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type Tokens struct {
	User                  User
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
}
