package models

import (
	"errors"
	"strings"
	"time"
)

const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

type Household struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Membership struct {
	UserID      string    `json:"user_id"`
	HouseholdID string    `json:"household_id"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

// HouseholdDetail is what GET /api/households/current returns.
type HouseholdDetail struct {
	Household
	Members []MemberView `json:"members"`
}

// MemberView is the public-facing member entry in HouseholdDetail.
type MemberView struct {
	UserID   string    `json:"user_id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type Invite struct {
	Code        string     `json:"code"`
	HouseholdID string     `json:"household_id"`
	InvitedBy   string     `json:"invited_by"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedAt      *time.Time `json:"used_at"`
	UsedBy      *string    `json:"used_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

type JoinRequest struct {
	Code string `json:"code"`
}

func (r *JoinRequest) Validate() error {
	r.Code = strings.TrimSpace(r.Code)
	if r.Code == "" {
		return errors.New("invite code is required")
	}
	return nil
}
