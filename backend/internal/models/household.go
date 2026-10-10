package models

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
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

// MembershipSummary is one row of the GET /api/households response —
// just enough for the frontend to render the switcher list.
type MembershipSummary struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
	IsActive bool      `json:"is_active"`
}

type SwitchHouseholdRequest struct {
	HouseholdID string `json:"household_id"`
}

func (r *SwitchHouseholdRequest) Validate() error {
	r.HouseholdID = strings.TrimSpace(r.HouseholdID)
	if r.HouseholdID == "" {
		return errors.New("household_id is required")
	}
	return nil
}

type RenameHouseholdRequest struct {
	Name string `json:"name"`
}

// Validate trims the name and enforces 1..80 chars. The upper bound keeps
// the household name displayable in the header switcher without truncation
// games on small screens.
func (r *RenameHouseholdRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(r.Name) > 80 {
		return errors.New("name must be 80 characters or fewer")
	}
	return nil
}

type TransferOwnershipRequest struct {
	ToUserID string `json:"to_user_id"`
}

func (r *TransferOwnershipRequest) Validate() error {
	r.ToUserID = strings.TrimSpace(r.ToUserID)
	if r.ToUserID == "" {
		return errors.New("to_user_id is required")
	}
	return nil
}

type CreateHouseholdRequest struct {
	Name string `json:"name"`
}

func (r *CreateHouseholdRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(r.Name) > 80 {
		return errors.New("name must be 80 characters or fewer")
	}
	return nil
}
