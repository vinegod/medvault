package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// AlertsHandler holds dependencies for the alerts endpoint.
type AlertsHandler struct {
	DB *sql.DB
}

// alertsResponse is what GET /api/alerts returns.
type alertsResponse struct {
	Expired      []models.Medicine `json:"expired"`
	ExpiringSoon []models.Medicine `json:"expiring_soon"`
	Summary      alertsSummary     `json:"summary"`
}

type alertsSummary struct {
	Expired              int `json:"expired"`
	ExpiringWithin30Days int `json:"expiring_within_30_days"`
	ExpiringWithin90Days int `json:"expiring_within_90_days"`
}

// Alerts handles GET /api/alerts.
// Returns expired and soon-to-expire medicines for the caller's household.
// Bucket thresholds come from the caller's user_settings.alert_days
// (the user *viewing* the page); falls back to defaults if unset.
func (h *AlertsHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	userID := middleware.UserIDFrom(r.Context())
	now := time.Now().UTC()

	innerDays, outerDays := h.alertThresholds(r, userID)

	// Fetch all non-deleted medicines that have an expiration date,
	// ordered so the most urgent appear first.
	rows, err := h.DB.QueryContext(r.Context(),
		medicineSelectFrom+` WHERE household_id = ?
		   AND deleted_at IS NULL
		   AND expiration_date IS NOT NULL
		 ORDER BY expiration_date ASC`,
		householdID,
	)
	if err != nil {
		api.ServerError(w, "Alerts: query", err)
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	resp := alertsResponse{
		Expired:      []models.Medicine{},
		ExpiringSoon: []models.Medicine{},
	}

	today := now.Format("2006-01-02")
	in30 := now.AddDate(0, 0, innerDays).Format("2006-01-02")
	in90 := now.AddDate(0, 0, outerDays).Format("2006-01-02")

	for rows.Next() {
		var m models.Medicine
		if err := scanMedicine(rows, &m); err != nil {
			api.ServerError(w, "Alerts: scan", err)
			return
		}

		// Defence-in-depth: the SELECT filters NULL expiration_date, but a
		// scan should never panic if that invariant ever loosens.
		if m.ExpirationDate == nil {
			continue
		}
		exp := *m.ExpirationDate

		switch {
		case exp < today:
			resp.Expired = append(resp.Expired, m)
			resp.Summary.Expired++

		case exp <= in30:
			resp.ExpiringSoon = append(resp.ExpiringSoon, m)
			resp.Summary.ExpiringWithin30Days++
			resp.Summary.ExpiringWithin90Days++

		case exp <= in90:
			resp.ExpiringSoon = append(resp.ExpiringSoon, m)
			resp.Summary.ExpiringWithin90Days++
		}
	}
	if err := rows.Err(); err != nil {
		api.ServerError(w, "Alerts: rows", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}

// alertThresholds reads the caller's alert_days from user_settings and
// returns (inner, outer) day counts. Inner is the smallest entry (the
// "expiring within N" bucket); outer is the largest (the upper cutoff
// past which medicines aren't reported as soon-to-expire). Fallback
// to the same defaults the settings handler uses when no row exists or
// the JSON is corrupt — alerts must keep working even if settings are
// unavailable.
func (h *AlertsHandler) alertThresholds(r *http.Request, userID string) (inner, outer int) {
	defaults := defaultSettings.AlertDays

	var raw string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT alert_days FROM user_settings WHERE user_id = ?`,
		userID,
	).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("Alerts: read user_settings", "user_id", userID, "err", err)
		}
		return defaults[0], defaults[len(defaults)-1]
	}

	var days []int
	if err := json.Unmarshal([]byte(raw), &days); err != nil || len(days) == 0 {
		slog.Warn("Alerts: corrupt alert_days, using defaults", "user_id", userID)
		return defaults[0], defaults[len(defaults)-1]
	}
	sort.Ints(days)
	return days[0], days[len(days)-1]
}
