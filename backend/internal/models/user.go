package models

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// usernameRE allows 3-30 characters: letters, digits, underscore.
var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // never serialised
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *RegisterRequest) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	if !usernameRE.MatchString(r.Username) {
		return errors.New("username must be 3-30 characters: letters, digits, underscore")
	}
	if len(r.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *LoginRequest) Validate() error {
	r.Username = strings.TrimSpace(r.Username)
	if r.Username == "" {
		return errors.New("username is required")
	}
	if r.Password == "" {
		return errors.New("password is required")
	}
	return nil
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (r *RefreshRequest) Validate() error {
	if strings.TrimSpace(r.RefreshToken) == "" {
		return errors.New("refresh_token is required")
	}
	return nil
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (r *ChangePasswordRequest) Validate() error {
	if r.CurrentPassword == "" {
		return errors.New("current_password is required")
	}
	if len(r.NewPassword) < 8 {
		return errors.New("new_password must be at least 8 characters")
	}
	if r.CurrentPassword == r.NewPassword {
		return errors.New("new_password must differ from current password")
	}
	return nil
}
