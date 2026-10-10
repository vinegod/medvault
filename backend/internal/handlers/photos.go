package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

const (
	photoMaxBytes = 5 << 20 // 5 MB
)

// PhotoHandler serves and stores medicine photos out-of-band of the sync
// envelope. Photos are content-addressed by SHA-256 and written to disk
// under PhotoDir; the medicines row stores only the hex hash so the same
// image referenced by multiple medicines deduplicates automatically.
type PhotoHandler struct {
	DB       *sql.DB
	PhotoDir string
}

// Upload handles POST /api/medicines/{id}/photo (multipart, field "photo").
// Replaces any existing photo on the medicine. The previous blob is left on
// disk because deduplication makes ownership unclear; a periodic GC sweep
// (out of scope for now) would reclaim orphaned files by diffing the
// photos directory against the photo_hash column.
func (h *PhotoHandler) Upload(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	// Verify the medicine belongs to the caller's household before doing
	// any I/O so a wrong-household upload doesn't waste disk on a write
	// we'd then have to undo.
	var exists int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM medicines WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		id, householdID,
	).Scan(&exists); err != nil {
		api.ServerError(w, "Photo upload: existence check", err)
		return
	}
	if exists == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, photoMaxBytes)
	//nolint:gosec // G120: body already bounded by MaxBytesReader above
	if err := r.ParseMultipartForm(photoMaxBytes); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation,
			"invalid form or photo too large (max 5 MB)")
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "photo field is required")
		return
	}
	defer file.Close() //nolint:errcheck // best-effort cleanup

	// Trust the Content-Type only enough to reject obviously-non-image
	// uploads. Real validation is byte-sniffing via http.DetectContentType
	// after we read the bytes — it inspects magic numbers, not just the
	// claimed type. Either way we re-encode to JPEG via the client before
	// upload, so even a content-type mismatch produces something safe.
	ct := header.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "image/") {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "photo must be an image")
		return
	}

	body, err := io.ReadAll(file)
	if err != nil {
		api.ServerError(w, "Photo upload: read body", err)
		return
	}

	if sniff := http.DetectContentType(body); !strings.HasPrefix(sniff, "image/") {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "photo must be an image")
		return
	}

	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	if err := h.writeBlob(hash, body); err != nil {
		api.ServerError(w, "Photo upload: write blob", err)
		return
	}

	now := time.Now().UTC()
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE medicines SET photo_hash = ?, updated_at = ?, synced_at = ?
		 WHERE id = ? AND household_id = ?`,
		hash, now, now, id, householdID,
	); err != nil {
		api.ServerError(w, "Photo upload: update row", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"photo_hash": hash})
}

// Get handles GET /api/medicines/{id}/photo. Streams the blob with a
// content-hash ETag so clients (and IndexedDB caches) can short-circuit.
func (h *PhotoHandler) Get(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	var hash sql.NullString
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT photo_hash FROM medicines
		  WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		id, householdID,
	).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}
	if err != nil {
		api.ServerError(w, "Photo get: query", err)
		return
	}
	if !hash.Valid || hash.String == "" {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "no photo for this medicine")
		return
	}

	path := h.blobPath(hash.String)
	f, err := os.Open(path) //nolint:gosec // path is constructed from validated hash
	if err != nil {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "photo file missing")
		slog.Warn("photo file missing on disk", "hash", hash.String, "path", path)
		return
	}
	defer f.Close() //nolint:errcheck // best-effort close on read-only file

	info, err := f.Stat()
	if err != nil {
		api.ServerError(w, "Photo get: stat", err)
		return
	}

	// No Content-Type: ServeContent sniffs it from the bytes. Upload accepts
	// any sniffed image/* type, so hardcoding image/jpeg mislabels PNGs etc.
	w.Header().Set("ETag", `"`+hash.String+`"`)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// Delete handles DELETE /api/medicines/{id}/photo. Clears the row's
// photo_hash; the blob on disk is intentionally left alone (other rows
// might reference the same hash). Out-of-scope: a sweep job that prunes
// orphaned blobs by diffing PhotoDir against the photo_hash column.
func (h *PhotoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	now := time.Now().UTC()
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE medicines SET photo_hash = NULL, updated_at = ?, synced_at = ?
		 WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		now, now, id, householdID,
	)
	if err != nil {
		api.ServerError(w, "Photo delete: update", err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}
	api.WriteJSON(w, http.StatusOK, nil)
}

func (h *PhotoHandler) blobPath(hash string) string {
	// Validated upstream — sha256 hex is always 64 lowercase hex chars,
	// so the join can't escape PhotoDir.
	return filepath.Join(h.PhotoDir, hash+".jpg")
}

// writeBlob writes content to the deduplicated path. If a file with the
// same hash already exists we skip rewriting (content is identical).
func (h *PhotoHandler) writeBlob(hash string, body []byte) error {
	if err := os.MkdirAll(h.PhotoDir, 0o750); err != nil {
		return fmt.Errorf("create photo dir: %w", err)
	}
	path := h.blobPath(hash)
	if _, err := os.Stat(path); err == nil {
		return nil // already on disk — content-addressable dedup
	}
	// Write to a temp file in the same dir, then rename, so a concurrent
	// reader either sees the old file or the complete new one.
	tmp, err := os.CreateTemp(h.PhotoDir, "upload-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write photo: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close photo: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("publish photo: %w", err)
	}
	return nil
}
