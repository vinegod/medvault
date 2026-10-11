package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/assistant"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// AIHandler serves the /api/ai endpoints.
type AIHandler struct {
	DB        *sql.DB
	Assistant *assistant.Assistant
}

var scanImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// aiSettingsResponse is GET /api/ai/settings. BaseURL is shown to admins only.
type aiSettingsResponse struct {
	assistant.Settings
	CanEdit bool `json:"can_edit"`
}

// Status handles GET /api/ai/status.
func (h *AIHandler) Status(w http.ResponseWriter, r *http.Request) {
	st, err := h.Assistant.Status(r.Context())
	if err != nil {
		api.ServerError(w, "AI status", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, st)
}

// GetSettings handles GET /api/ai/settings.
func (h *AIHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	admin, err := database.IsAdmin(r.Context(), h.DB, middleware.UserIDFrom(r.Context()))
	if err != nil {
		api.ServerError(w, "AI settings: admin check", err)
		return
	}
	s, err := h.Assistant.Settings(r.Context())
	if err != nil {
		api.ServerError(w, "AI settings: load", err)
		return
	}
	if !admin {
		s.BaseURL = ""
	}
	api.WriteJSON(w, http.StatusOK, aiSettingsResponse{Settings: s, CanEdit: admin})
}

// UpdateSettings handles PUT /api/ai/settings (admin only).
func (h *AIHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req models.AISettingsRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON body")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	if err := h.Assistant.SaveSettings(r.Context(), middleware.UserIDFrom(r.Context()), &req); err != nil {
		writeAIError(w, "AI settings: save", err)
		return
	}
	h.GetSettings(w, r)
}

// ResetSettings handles DELETE /api/ai/settings (admin only).
func (h *AIHandler) ResetSettings(w http.ResponseWriter, r *http.Request) {
	if err := h.Assistant.ResetSettings(r.Context()); err != nil {
		api.ServerError(w, "AI settings: reset", err)
		return
	}
	h.GetSettings(w, r)
}

// Scan handles POST /api/ai/scan (multipart, field "photo").
func (h *AIHandler) Scan(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, photoMaxBytes)
	//nolint:gosec // G120: body already bounded by MaxBytesReader above
	if err := r.ParseMultipartForm(photoMaxBytes); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation,
			"invalid form or photo too large (max 5 MB)")
		return
	}
	file, _, err := r.FormFile("photo")
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "photo field is required")
		return
	}
	defer file.Close() //nolint:errcheck // best-effort cleanup

	body, err := io.ReadAll(file)
	if err != nil {
		api.ServerError(w, "AI scan: read body", err)
		return
	}
	mimeType := http.DetectContentType(body)
	if !scanImageTypes[mimeType] {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "photo must be JPEG, PNG or WebP")
		return
	}

	h.extendWriteDeadline(w)
	res, err := h.Assistant.Scan(r.Context(), middleware.HouseholdIDFrom(r.Context()),
		assistant.Image{MIMEType: mimeType, Data: body})
	if err != nil {
		writeAIError(w, "AI scan", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, res)
}

// Chat handles POST /api/ai/chat.
func (h *AIHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req models.ChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON body")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	h.extendWriteDeadline(w)
	reply, err := h.Assistant.Chat(r.Context(), middleware.HouseholdIDFrom(r.Context()), req.Messages)
	if err != nil {
		writeAIError(w, "AI chat", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, reply)
}

// extendWriteDeadline lets a model call outlive the server's WriteTimeout.
func (h *AIHandler) extendWriteDeadline(w http.ResponseWriter) {
	deadline := time.Now().Add(h.Assistant.Timeout() + 10*time.Second)
	if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil {
		slog.Debug("AI: extend write deadline", "err", err)
	}
}

func writeAIError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, assistant.ErrDisabled):
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "assistant is not configured")
	case errors.Is(err, assistant.ErrKeyUnreadable):
		api.WriteError(
			w,
			http.StatusConflict,
			models.CodeConflict,
			"stored API key cannot be decrypted; enter it again",
		)
	case errors.Is(err, assistant.ErrAPIKeyRequired):
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "api_key is required for this provider")
	case errors.Is(err, assistant.ErrUpstream):
		api.WriteError(w, http.StatusBadGateway, models.CodeUpstream, "AI provider request failed")
	case errors.Is(err, context.DeadlineExceeded):
		api.WriteError(w, http.StatusGatewayTimeout, models.CodeTimeout, "AI provider timed out")
	default:
		api.ServerError(w, op, err)
	}
}
