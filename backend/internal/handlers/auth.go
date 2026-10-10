package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// Registration modes for AuthHandler.RegistrationMode. The zero value
// behaves as RegistrationOpen.
const (
	RegistrationOpen   = "open"
	RegistrationInvite = "invite"
	RegistrationClosed = "closed"
)

// AuthHandler holds dependencies for all authentication endpoints.
type AuthHandler struct {
	DB               *sql.DB
	JWTSecret        string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	RegistrationMode string
}

// authResponse matches the shape frontend/src/types/index.ts AuthResponse expects.
type authResponse struct {
	User struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"user"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// tokenResponse is the slimmer shape returned by /auth/refresh.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Register creates a user plus either a personal household or, with an
// invite code, a membership in the invited household, in one transaction.
// RegistrationMode can require an invite or close signup; the first user
// can always register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	var userCount int
	if err := h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		api.ServerError(w, "register: count users", err)
		return
	}
	if status, code, msg, blocked := registrationBlocked(
		h.RegistrationMode,
		userCount == 0,
		req.InviteCode != "",
	); blocked {
		api.WriteError(w, status, code, msg)
		return
	}

	// Check username availability.
	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE username = ?`, req.Username,
	).Scan(&existing); err != nil {
		api.ServerError(w, "register: check username", err)
		return
	}
	if existing > 0 {
		api.WriteError(w, http.StatusConflict, models.CodeConflict, "username already taken")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		api.ServerError(w, "register: hash password", err)
		return
	}

	userID := uuid.NewString()
	now := time.Now().UTC()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		api.ServerError(w, "register: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // superseded by Commit on success

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO users (id, username, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, req.Username, hash, now, now,
	); err != nil {
		api.ServerError(w, "register: insert user", err)
		return
	}

	if req.InviteCode == "" {
		err = createPersonalHousehold(r.Context(), tx, userID, req.Username, now)
	} else {
		_, err = joinWithInvite(r.Context(), tx, req.InviteCode, userID, req.Username, now)
	}
	if errors.Is(err, errInviteInvalid) {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid or expired invite code")
		return
	}
	if err != nil {
		api.ServerError(w, "register", err)
		return
	}

	if err = tx.Commit(); err != nil {
		api.ServerError(w, "register: commit", err)
		return
	}

	resp, ok := h.issueTokens(w, r, userID, req.Username, now)
	if !ok {
		return
	}

	api.WriteJSON(w, http.StatusCreated, resp)
}

// registrationBlocked reports whether signup must be refused under mode,
// and with which error. The first user is always let through.
func registrationBlocked(mode string, firstUser, hasInvite bool) (status int, code, msg string, blocked bool) {
	if firstUser {
		return 0, "", "", false
	}
	switch mode {
	case RegistrationClosed:
		return http.StatusForbidden, models.CodeRegistrationClosed, "registration is closed on this server", true
	case RegistrationInvite:
		if !hasInvite {
			return http.StatusBadRequest, models.CodeInviteRequired, "an invite code is required to register", true
		}
	}
	return 0, "", "", false
}

// createPersonalHousehold makes userID the owner of a new
// "<username>'s Household" and sets it as their active household.
func createPersonalHousehold(ctx context.Context, tx *sql.Tx, userID, username string, now time.Time) error {
	householdID := uuid.NewString()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO households (id, name, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		householdID, username+"'s Household", userID, now, now,
	); err != nil {
		return fmt.Errorf("insert household: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		userID, householdID, models.RoleOwner, now,
	); err != nil {
		return fmt.Errorf("insert membership: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		householdID, userID,
	); err != nil {
		return fmt.Errorf("set active household: %w", err)
	}
	return nil
}

// Login verifies credentials and returns a fresh token pair.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	var user models.User
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, username, password_hash, created_at FROM users WHERE username = ?`,
		req.Username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)

	// Don't leak which usernames exist: same error response for both cases,
	// and same response time. When the user doesn't exist we still run a
	// bcrypt comparison (against a dummy hash) so the wall-clock cost
	// matches the "user exists, wrong password" path.
	if errors.Is(err, sql.ErrNoRows) {
		auth.EqualizeTimingForMissingUser(req.Password)
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		api.ServerError(w, "login: query user", err)
		return
	}
	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "invalid credentials")
		return
	}

	resp, ok := h.issueTokens(w, r, user.ID, user.Username, user.CreatedAt)
	if !ok {
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// Refresh rotates the refresh token: old one is deleted, new pair issued.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req models.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	// Tokens issued before the "typ" claim existed carry an empty type; they
	// are still checked against refresh_tokens below, so only reject an
	// explicit access token here.
	claims, err := auth.ValidateToken(h.JWTSecret, req.RefreshToken)
	if err != nil || claims.Type == auth.TokenTypeAccess {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "invalid or expired refresh token")
		return
	}

	tokenHash := auth.HashToken(req.RefreshToken)

	// Confirm the token exists, belongs to this user, and hasn't been revoked.
	var storedID string
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM refresh_tokens
		 WHERE token_hash = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?`,
		tokenHash, claims.UserID, time.Now().UTC(),
	).Scan(&storedID)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "refresh token not recognised or revoked")
		return
	}
	if err != nil {
		api.ServerError(w, "refresh: query token", err)
		return
	}

	var username string
	if err = h.DB.QueryRowContext(r.Context(),
		`SELECT username FROM users WHERE id = ?`, claims.UserID,
	).Scan(&username); err != nil {
		api.ServerError(w, "refresh: query user", err)
		return
	}

	// Delete old token before issuing new pair (rotation). Two concurrent
	// refreshes with the same token both pass the SELECT above; only the one
	// whose DELETE actually removes the row may mint a new pair.
	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM refresh_tokens WHERE id = ?`, storedID,
	)
	if err != nil {
		api.ServerError(w, "refresh: delete old token", err)
		return
	}
	if n, err := res.RowsAffected(); err != nil {
		api.ServerError(w, "refresh: delete old token", err)
		return
	} else if n == 0 {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "refresh token not recognised or revoked")
		return
	}

	accessToken, err := auth.GenerateAccessToken(h.JWTSecret, claims.UserID, username, h.AccessTTL)
	if err != nil {
		api.ServerError(w, "refresh: generate access token", err)
		return
	}

	refreshToken, err := auth.GenerateRefreshToken(h.JWTSecret, claims.UserID, h.RefreshTTL)
	if err != nil {
		api.ServerError(w, "refresh: generate refresh token", err)
		return
	}

	if !h.storeRefreshToken(w, r, claims.UserID, refreshToken) {
		return
	}

	api.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	})
}

// Logout revokes the refresh token. Always succeeds from the client's view.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req models.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.RefreshToken != "" {
		tokenHash := auth.HashToken(req.RefreshToken)
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ?`,
			time.Now().UTC(), tokenHash,
		)
	}

	api.WriteJSON(w, http.StatusOK, nil)
}

// ChangePassword verifies the caller's current password, replaces it with the
// new one, revokes all existing refresh tokens (forcing re-login on other
// devices), and returns a fresh token pair so the current session stays active.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())
	username := middleware.UsernameFrom(r.Context())

	var req models.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	var currentHash string
	var createdAt time.Time
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT password_hash, created_at FROM users WHERE id = ?`, userID,
	).Scan(&currentHash, &createdAt); err != nil {
		api.ServerError(w, "changePassword: query user", err)
		return
	}

	if !auth.CheckPassword(req.CurrentPassword, currentHash) {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		api.ServerError(w, "changePassword: hash password", err)
		return
	}

	if err := database.SetPassword(r.Context(), h.DB, username, newHash); err != nil {
		api.ServerError(w, "changePassword", err)
		return
	}

	// Issue a fresh pair so the caller stays logged in on this device.
	resp, ok := h.issueTokens(w, r, userID, username, createdAt)
	if !ok {
		return
	}
	api.WriteJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
	})
}

// issueTokens generates and stores a fresh access+refresh pair.
// Returns (response, true) on success; on failure it writes the error and returns false.
func (h *AuthHandler) issueTokens(
	w http.ResponseWriter,
	r *http.Request,
	userID, username string,
	createdAt time.Time,
) (authResponse, bool) {
	accessToken, err := auth.GenerateAccessToken(h.JWTSecret, userID, username, h.AccessTTL)
	if err != nil {
		api.ServerError(w, "issueTokens: generate access token", err)
		return authResponse{}, false
	}

	refreshToken, err := auth.GenerateRefreshToken(h.JWTSecret, userID, h.RefreshTTL)
	if err != nil {
		api.ServerError(w, "issueTokens: generate refresh token", err)
		return authResponse{}, false
	}

	if !h.storeRefreshToken(w, r, userID, refreshToken) {
		return authResponse{}, false
	}

	var resp authResponse
	resp.User.ID = userID
	resp.User.Username = username
	resp.User.CreatedAt = createdAt
	resp.AccessToken = accessToken
	resp.RefreshToken = refreshToken

	return resp, true
}

// storeRefreshToken inserts a hashed refresh token into the DB.
// Returns true on success; on failure it writes the error response.
func (h *AuthHandler) storeRefreshToken(w http.ResponseWriter, r *http.Request, userID, rawToken string) bool {
	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at)
		 VALUES (?, ?, ?, ?)`,
		uuid.NewString(), userID, auth.HashToken(rawToken),
		time.Now().UTC().Add(h.RefreshTTL),
	)
	if err != nil {
		api.ServerError(w, "storeRefreshToken", err)
		return false
	}

	return true
}
