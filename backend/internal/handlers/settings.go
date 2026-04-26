package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// SettingsHandler holds dependencies for the settings endpoints.
type SettingsHandler struct {
	DB *sql.DB
}

var defaultSettings = models.UserSettings{
	AlertDays:          []int{30, 60, 90},
	NotificationMethod: "browser",
	DefaultCategory:    "general",
}

// GetSettings handles GET /api/settings.
// Returns the caller's settings, falling back to defaults if none are stored.
func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())

	var alertDaysJSON, notifMethod, defaultCat string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alert_days, notification_method, default_category
		 FROM user_settings WHERE user_id = ?`,
		userID,
	).Scan(&alertDaysJSON, &notifMethod, &defaultCat)

	if err == sql.ErrNoRows {
		api.WriteJSON(w, http.StatusOK, defaultSettings)
		return
	}
	if err != nil {
		slog.Error("GetSettings: query", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	var alertDays []int
	if err := json.Unmarshal([]byte(alertDaysJSON), &alertDays); err != nil {
		// Corrupt DB value — return defaults rather than a 500.
		slog.Warn("GetSettings: corrupt alert_days JSON, using defaults", "user_id", userID)
		alertDays = defaultSettings.AlertDays
	}

	api.WriteJSON(w, http.StatusOK, models.UserSettings{
		AlertDays:          alertDays,
		NotificationMethod: notifMethod,
		DefaultCategory:    defaultCat,
	})
}

// UpdateSettings handles PUT /api/settings.
// Upserts the caller's settings row.
func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())

	var req models.UserSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	alertDaysJSON, err := json.Marshal(req.AlertDays)
	if err != nil {
		slog.Error("UpdateSettings: marshal alert_days", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO user_settings (user_id, alert_days, notification_method, default_category, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   alert_days          = excluded.alert_days,
		   notification_method = excluded.notification_method,
		   default_category    = excluded.default_category,
		   updated_at          = excluded.updated_at`,
		userID, string(alertDaysJSON), req.NotificationMethod, req.DefaultCategory,
		time.Now().UTC(),
	)
	if err != nil {
		slog.Error("UpdateSettings: upsert", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, req)
}
