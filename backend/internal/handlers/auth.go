package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// AuthHandler holds dependencies for all authentication endpoints.
type AuthHandler struct {
	DB         *sql.DB
	JWTSecret  string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
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

// Register creates a user, a personal household, and an owner membership
// all in one transaction so a partial failure leaves no orphaned rows.
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

	// Check username availability.
	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE username = ?`, req.Username,
	).Scan(&existing); err != nil {
		slog.Error("register: check username", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	if existing > 0 {
		api.WriteError(w, http.StatusConflict, models.CodeConflict, "username already taken")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("register: hash password", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	userID := uuid.NewString()
	householdID := uuid.NewString()
	now := time.Now().UTC()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Error("register: begin tx", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer tx.Rollback() //nolint:errcheck // superseded by Commit on success

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO users (id, username, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, req.Username, hash, now, now,
	); err != nil {
		slog.Error("register: insert user", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO households (id, name, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		householdID, req.Username+"'s Household", userID, now, now,
	); err != nil {
		slog.Error("register: insert household", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		userID, householdID, models.RoleOwner, now,
	); err != nil {
		slog.Error("register: insert membership", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		householdID, userID,
	); err != nil {
		slog.Error("register: set active household", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if err = tx.Commit(); err != nil {
		slog.Error("register: commit", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	resp, ok := h.issueTokens(w, r, userID, req.Username, now)
	if !ok {
		return
	}

	api.WriteJSON(w, http.StatusCreated, resp)
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
	if err == sql.ErrNoRows {
		auth.EqualizeTimingForMissingUser(req.Password)
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		slog.Error("login: query user", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
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

	claims, err := auth.ValidateToken(h.JWTSecret, req.RefreshToken)
	if err != nil {
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
		slog.Error("refresh: query token", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	var username string
	if err = h.DB.QueryRowContext(r.Context(),
		`SELECT username FROM users WHERE id = ?`, claims.UserID,
	).Scan(&username); err != nil {
		slog.Error("refresh: query user", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	// Delete old token before issuing new pair (rotation).
	if _, err = h.DB.ExecContext(r.Context(),
		`DELETE FROM refresh_tokens WHERE id = ?`, storedID,
	); err != nil {
		slog.Error("refresh: delete old token", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	accessToken, err := auth.GenerateAccessToken(h.JWTSecret, claims.UserID, username, h.AccessTTL)
	if err != nil {
		slog.Error("refresh: generate access token", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	refreshToken, err := auth.GenerateRefreshToken(h.JWTSecret, claims.UserID, h.RefreshTTL)
	if err != nil {
		slog.Error("refresh: generate refresh token", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
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
		slog.Error("changePassword: query user", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if !auth.CheckPassword(req.CurrentPassword, currentHash) {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "current password is incorrect")
		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		slog.Error("changePassword: hash password", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	now := time.Now().UTC()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Error("changePassword: begin tx", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer tx.Rollback() //nolint:errcheck // superseded by Commit on success

	if _, err = tx.ExecContext(r.Context(),
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		newHash, now, userID,
	); err != nil {
		slog.Error("changePassword: update hash", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	// Revoke all existing refresh tokens so other devices are forced to re-login.
	if _, err = tx.ExecContext(r.Context(),
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		now, userID,
	); err != nil {
		slog.Error("changePassword: revoke tokens", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if err = tx.Commit(); err != nil {
		slog.Error("changePassword: commit", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
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
		slog.Error("issueTokens: generate access token", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return authResponse{}, false
	}

	refreshToken, err := auth.GenerateRefreshToken(h.JWTSecret, userID, h.RefreshTTL)
	if err != nil {
		slog.Error("issueTokens: generate refresh token", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
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
		slog.Error("storeRefreshToken", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return false
	}

	return true
}
