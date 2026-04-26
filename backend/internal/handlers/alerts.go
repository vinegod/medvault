package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
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
	Expired               int `json:"expired"`
	ExpiringWithin30Days  int `json:"expiring_within_30_days"`
	ExpiringWithin90Days  int `json:"expiring_within_90_days"`
}

// Alerts handles GET /api/alerts.
// Returns expired and soon-to-expire medicines for the caller's household.
// Threshold: 90 days ahead (matches the ExpirationSummary type in the frontend).
func (h *AlertsHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	now := time.Now().UTC()

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
		slog.Error("Alerts: query", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer rows.Close()

	resp := alertsResponse{
		Expired:      []models.Medicine{},
		ExpiringSoon: []models.Medicine{},
	}

	today := now.Format("2006-01-02")
	in30 := now.AddDate(0, 0, 30).Format("2006-01-02")
	in90 := now.AddDate(0, 0, 90).Format("2006-01-02")

	for rows.Next() {
		var m models.Medicine
		if err := scanMedicine(rows, &m); err != nil {
			slog.Error("Alerts: scan", "err", err)
			api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			return
		}

		exp := *m.ExpirationDate // safe: WHERE filters out NULL

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
		slog.Error("Alerts: rows", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, resp)
}
