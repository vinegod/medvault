package assistant

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/models"
)

// Image is a package photo.
type Image struct {
	MIMEType string
	Data     []byte
}

// ScanResult is a draft medicine read from a photo plus the household's
// medicines it may duplicate.
type ScanResult struct {
	Draft   models.MedicineRequest `json:"draft"`
	Matches []Match                `json:"matches"`
}

// Match is an existing medicine similar to a scanned draft.
type Match struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"` // barcode | name | ingredient
}

// scannedBox is the structured output requested from the vision model.
// All fields are required; "" or 0 means not printed.
type scannedBox struct {
	Name             string `json:"name"`
	ActiveIngredient string `json:"active_ingredient"`
	Quantity         int    `json:"quantity"`
	Unit             string `json:"unit"`
	ExpirationDate   string `json:"expiration_date"`
	Barcode          string `json:"barcode"`
}

// Scan reads a medicine package photo into a draft entry. Fields the model
// gets wrong are dropped, so the draft always passes validation except for
// a possibly empty name.
func (a *Assistant) Scan(ctx context.Context, householdID string, img Image) (ScanResult, error) {
	cfg, e, err := a.prepare(ctx)
	if err != nil {
		return ScanResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.opts.Timeout)
	defer cancel()

	dataURI := "data:" + img.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
	resp, err := genkit.Generate(ctx, e.g,
		ai.WithModelName(modelName(&cfg, cfg.visionModel())),
		ai.WithMessages(ai.NewUserMessage(ai.NewTextPart(scanPrompt), ai.NewMediaPart(img.MIMEType, dataURI))),
		ai.WithOutputType(scannedBox{}),
	)
	if err != nil {
		return ScanResult{}, upstreamError(ctx, "scan photo", err)
	}
	var box scannedBox
	if err := resp.Output(&box); err != nil {
		slog.Warn("scan photo: unparseable model output", "err", err)
		return ScanResult{}, ErrUpstream
	}

	draft := a.buildDraft(ctx, &box)
	items, err := a.loadInventory(ctx, householdID)
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{Draft: draft, Matches: findMatches(&draft, items)}, nil
}

func (a *Assistant) buildDraft(ctx context.Context, box *scannedBox) models.MedicineRequest {
	draft := models.MedicineRequest{
		Name:     strings.TrimSpace(box.Name),
		Quantity: box.Quantity,
		Unit:     normalizeUnit(box.Unit),
		Barcode:  strings.TrimSpace(box.Barcode),
	}
	if v := strings.TrimSpace(box.ActiveIngredient); v != "" {
		draft.ActiveIngredient = &v
	}
	if v := normalizeDate(box.ExpirationDate); v != "" {
		draft.ExpirationDate = &v
	}

	if d, err := a.drugs.LookupExact(ctx, a.opts.DrugCountry, draft.Name); err != nil {
		slog.Warn("scan photo: drug lookup", "err", err)
	} else if d != nil && draft.ActiveIngredient == nil {
		if inn := firstNonEmpty(deref(d.INN), deref(d.Ingredients)); inn != "" {
			draft.ActiveIngredient = &inn
		}
	}

	dropInvalid(&draft)
	if days := database.DefaultDaysAfterOpening(draft.Unit); days > 0 {
		draft.DaysAfterOpening = &days
	}
	return draft
}

// dropInvalid resets each field that fails validation on its own.
func dropInvalid(d *models.MedicineRequest) {
	valid := func(set func(r *models.MedicineRequest)) bool {
		probe := models.MedicineRequest{Name: "x", Unit: "tablets"}
		set(&probe)
		return probe.Validate() == nil
	}
	if !valid(func(r *models.MedicineRequest) { r.Name = d.Name }) {
		d.Name = ""
	}
	if !valid(func(r *models.MedicineRequest) { r.Unit = d.Unit }) {
		d.Unit = "tablets"
	}
	if !valid(func(r *models.MedicineRequest) { r.Quantity = d.Quantity }) {
		d.Quantity = 0
	}
	if !valid(func(r *models.MedicineRequest) { r.Barcode = d.Barcode }) {
		d.Barcode = ""
	}
	if !valid(func(r *models.MedicineRequest) { r.ActiveIngredient = d.ActiveIngredient }) {
		d.ActiveIngredient = nil
	}
	if !valid(func(r *models.MedicineRequest) { r.ExpirationDate = d.ExpirationDate }) {
		d.ExpirationDate = nil
	}
}

// unitStems maps a singular stem to its unit; ml, mg and g match exactly.
var unitStems = []struct{ stem, unit string }{
	{"tablet", "tablets"},
	{"capsule", "capsules"},
	{"piece", "pieces"},
	{"drop", "drops"},
	{"sachet", "sachets"},
	{"patch", "patches"},
}

// normalizeUnit maps near-misses such as "Tablet" or "capsuless" to a
// known unit. Anything else is returned lowercased.
func normalizeUnit(s string) string {
	u := strings.ToLower(strings.TrimSpace(s))
	for _, st := range unitStems {
		if strings.HasPrefix(u, st.stem) {
			return st.unit
		}
	}
	return u
}

var monthYearRE = regexp.MustCompile(`^(\d{1,2})[./-](\d{4})$|^(\d{4})[./-](\d{1,2})$`)

// normalizeDate turns YYYY-MM-DD, MM/YYYY or YYYY-MM into YYYY-MM-DD; a
// month-only date becomes the month's last day. Anything else is "".
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.Format(time.DateOnly)
	}
	m := monthYearRE.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	month, year := m[1], m[2]
	if m[3] != "" {
		year, month = m[3], m[4]
	}
	t, err := time.Parse("2006-1", year+"-"+strings.TrimLeft(month, "0"))
	if err != nil {
		return ""
	}
	return t.AddDate(0, 1, -1).Format(time.DateOnly)
}

func findMatches(d *models.MedicineRequest, items []stockItem) []Match {
	matches := make([]Match, 0, len(items))
	ingredient := strings.ToLower(strings.TrimSpace(deref(d.ActiveIngredient)))
	for i := range items {
		it := &items[i]
		var reason string
		switch {
		case d.Barcode != "" && it.Barcode == d.Barcode:
			reason = "barcode"
		case d.Name != "" && strings.EqualFold(it.Name, d.Name):
			reason = "name"
		case ingredient != "" && strings.EqualFold(strings.TrimSpace(it.ActiveIngredient), ingredient):
			reason = "ingredient"
		default:
			continue
		}
		matches = append(matches, Match{ID: it.ID, Name: it.Name, Reason: reason})
	}
	return matches
}

// upstreamError logs a provider failure and returns ErrUpstream, or the
// deadline error on timeout.
func upstreamError(ctx context.Context, op string, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", op, context.DeadlineExceeded)
	}
	slog.Warn(op+": provider error", "err", err)
	return ErrUpstream
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
