package assistant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/vinegod/medvault/backend/internal/database"
)

type ctxKey int

const (
	householdKey ctxKey = iota
	seenKey
)

// withHousehold scopes every tool call made under ctx to householdID and
// records the medicines the tools return.
func withHousehold(ctx context.Context, householdID string) (context.Context, *seenItems) {
	seen := &seenItems{names: make(map[string]string)}
	ctx = context.WithValue(ctx, householdKey, householdID)
	return context.WithValue(ctx, seenKey, seen), seen
}

func householdFrom(ctx context.Context) (string, error) {
	id, _ := ctx.Value(householdKey).(string)
	if id == "" {
		return "", fmt.Errorf("tool called without a household")
	}
	return id, nil
}

// seenItems collects the inventory items tools returned during one chat.
type seenItems struct {
	mu    sync.Mutex
	names map[string]string // id -> name
}

func (s *seenItems) add(items []stockItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range items {
		s.names[items[i].ID] = items[i].Name
	}
}

func recordSeen(ctx context.Context, items []stockItem) {
	if s, ok := ctx.Value(seenKey).(*seenItems); ok {
		s.add(items)
	}
}

// stockItem is one medicine as tools present it to the model.
type stockItem struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ActiveIngredient string `json:"active_ingredient,omitempty"`
	Quantity         int    `json:"quantity"`
	Unit             string `json:"unit"`
	Category         string `json:"category,omitempty"`
	Notes            string `json:"notes,omitempty"`
	ExpiresOn        string `json:"expires_on,omitempty"`
	Expired          bool   `json:"expired,omitempty"`
	Barcode          string `json:"-"`
}

const (
	inventoryLimit = 100
	notesLimit     = 200
)

// loadInventory returns the household's non-deleted medicines.
func (a *Assistant) loadInventory(ctx context.Context, householdID string) ([]stockItem, error) {
	rows, err := a.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(active_ingredient, ''), quantity, unit,
		       COALESCE(category, ''), COALESCE(notes, ''), COALESCE(barcode, ''),
		       expiration_date, opened_at, days_after_opening
		FROM medicines WHERE household_id = ? AND deleted_at IS NULL
		ORDER BY name`, householdID)
	if err != nil {
		return nil, fmt.Errorf("query inventory: %w", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	today := time.Now().UTC().Format(time.DateOnly)
	var items []stockItem
	for rows.Next() {
		var (
			it                 stockItem
			expiration, opened *string
			daysAfterOpening   *int
		)
		if err := rows.Scan(&it.ID, &it.Name, &it.ActiveIngredient, &it.Quantity, &it.Unit,
			&it.Category, &it.Notes, &it.Barcode, &expiration, &opened, &daysAfterOpening); err != nil {
			return nil, fmt.Errorf("scan inventory: %w", err)
		}
		it.Notes = truncate(it.Notes, notesLimit)
		it.ExpiresOn = effectiveExpiry(expiration, opened, daysAfterOpening)
		it.Expired = it.ExpiresOn != "" && it.ExpiresOn < today
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate inventory: %w", err)
	}
	return items, nil
}

// effectiveExpiry is the earlier of the printed date and opened + days.
func effectiveExpiry(expiration, opened *string, daysAfterOpening *int) string {
	var out string
	if expiration != nil {
		out = *expiration
	}
	if opened == nil || daysAfterOpening == nil {
		return out
	}
	t, err := time.Parse(time.DateOnly, *opened)
	if err != nil {
		return out
	}
	if afterOpening := t.AddDate(0, 0, *daysAfterOpening).Format(time.DateOnly); out == "" || afterOpening < out {
		return afterOpening
	}
	return out
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

type inventoryQuery struct {
	Text string `json:"text,omitempty" jsonschema_description:"Words to match in name, ingredient, category, notes; empty lists all."`
}

type drugQuery struct {
	Text string `json:"text" jsonschema_description:"Trade name, INN or ingredient to look up."`
}

type similarQuery struct {
	Name string `json:"name" jsonschema_description:"Trade name of the medicine to find alternatives for."`
}

type similarResult struct {
	Reference    *database.Drug  `json:"reference,omitempty"`
	InStock      []stockItem     `json:"in_stock"`
	Alternatives []database.Drug `json:"alternatives"`
}

func (a *Assistant) defineTools(g *genkit.Genkit) []ai.ToolRef {
	return []ai.ToolRef{
		genkit.DefineTool(
			g,
			"search_inventory",
			"List the household medicine inventory, optionally filtered by text. Returns id, name, active ingredient, "+
				"quantity, unit, category, notes, effective expiry date and an expired flag.",
			func(tc *ai.ToolContext, q inventoryQuery) ([]stockItem, error) {
				return a.searchInventory(tc.Context, q)
			},
		),
		genkit.DefineTool(
			g,
			"search_drug_db",
			"Look up drugs in the national drug registry by trade name, INN or ingredient. Returns name, INN, ingredients and pharmacological group.",
			func(tc *ai.ToolContext, q drugQuery) ([]database.Drug, error) {
				return a.searchDrugDB(tc.Context, q.Text)
			},
		),
		genkit.DefineTool(
			g,
			"find_similar",
			"Find medicines with the same active ingredient or pharmacological group: those already in the household inventory and others from the drug registry.",
			func(tc *ai.ToolContext, q similarQuery) (similarResult, error) {
				return a.findSimilar(tc.Context, q.Name)
			},
		),
	}
}

func (a *Assistant) searchInventory(ctx context.Context, q inventoryQuery) ([]stockItem, error) {
	householdID, err := householdFrom(ctx)
	if err != nil {
		return nil, err
	}
	items, err := a.loadInventory(ctx, householdID)
	if err != nil {
		return nil, err
	}

	// No hits returns everything, so the model can match across languages.
	words := strings.Fields(strings.ToLower(q.Text))
	out := []stockItem{}
	for i := range items {
		if matchesAny(&items[i], words) {
			out = append(out, items[i])
		}
	}
	if len(out) == 0 && len(items) > 0 {
		out = items
	}
	if len(out) > inventoryLimit {
		out = out[:inventoryLimit]
	}
	recordSeen(ctx, out)
	return out, nil
}

// matchesAny reports whether any word occurs in the item's text fields.
// No words matches everything.
func matchesAny(it *stockItem, words []string) bool {
	if len(words) == 0 {
		return true
	}
	haystack := strings.ToLower(it.Name + " " + it.ActiveIngredient + " " + it.Category + " " + it.Notes)
	for _, w := range words {
		if strings.Contains(haystack, w) {
			return true
		}
	}
	return false
}

const drugToolLimit = 10

func (a *Assistant) searchDrugDB(ctx context.Context, text string) ([]database.Drug, error) {
	db, err := a.drugDB()
	if err != nil || db == nil {
		return []database.Drug{}, err
	}
	if utf8.RuneCountInString(strings.TrimSpace(text)) < 2 {
		return []database.Drug{}, nil
	}
	drugs, err := database.SearchDrugs(ctx, db, a.opts.DrugCountry, strings.TrimSpace(text), drugToolLimit)
	if err != nil {
		return nil, fmt.Errorf("search drug db: %w", err)
	}
	return drugs, nil
}

func (a *Assistant) findSimilar(ctx context.Context, name string) (similarResult, error) {
	householdID, err := householdFrom(ctx)
	if err != nil {
		return similarResult{}, err
	}
	ref, err := a.lookupDrug(ctx, name)
	if err != nil {
		return similarResult{}, err
	}
	inn, group := drugINN(ref), ""
	if ref != nil {
		group = deref(ref.PharmaGroup)
	}

	items, err := a.loadInventory(ctx, householdID)
	if err != nil {
		return similarResult{}, err
	}
	inStock, err := a.sameDrugInStock(ctx, items, name, inn, group)
	if err != nil {
		return similarResult{}, err
	}
	recordSeen(ctx, inStock)
	res := similarResult{Reference: ref, InStock: inStock, Alternatives: []database.Drug{}}

	if ref == nil || (inn == "" && group == "") {
		return res, nil
	}
	db, err := a.drugDB()
	if err != nil || db == nil {
		return res, err
	}
	alts, err := database.DrugsByGroup(ctx, db, a.opts.DrugCountry, inn, group, ref.Name, drugToolLimit)
	if err != nil {
		return similarResult{}, fmt.Errorf("find alternatives: %w", err)
	}
	if alts != nil {
		res.Alternatives = alts
	}
	return res, nil
}

// sameDrugInStock returns the items other than name that share its INN or,
// per the drug DB, its pharmacological group.
func (a *Assistant) sameDrugInStock(
	ctx context.Context,
	items []stockItem,
	name, inn, group string,
) ([]stockItem, error) {
	out := []stockItem{}
	for i := range items {
		it := &items[i]
		if strings.EqualFold(it.Name, name) {
			continue
		}
		if inn != "" && strings.Contains(strings.ToLower(it.ActiveIngredient), inn) {
			out = append(out, *it)
			continue
		}
		if group == "" {
			continue
		}
		d, err := a.drugs.LookupExact(ctx, a.opts.DrugCountry, it.Name)
		if err != nil {
			return nil, fmt.Errorf("look up %q: %w", it.Name, err)
		}
		if d != nil && deref(d.PharmaGroup) == group {
			out = append(out, *it)
		}
	}
	return out, nil
}

// lookupDrug finds name in the drug DB: an exact match, else the best
// search hit. nil when there is no drug DB or no match.
func (a *Assistant) lookupDrug(ctx context.Context, name string) (*database.Drug, error) {
	d, err := a.drugs.LookupExact(ctx, a.opts.DrugCountry, name)
	if err != nil {
		return nil, fmt.Errorf("look up %q: %w", name, err)
	}
	if d != nil {
		return d, nil
	}
	hits, err := a.searchDrugDB(ctx, name)
	if err != nil || len(hits) == 0 {
		return nil, err
	}
	return &hits[0], nil
}

func (a *Assistant) drugDB() (*sql.DB, error) {
	if a.drugs == nil || a.opts.DrugCountry == "" {
		return nil, nil
	}
	db, err := a.drugs.Get(a.opts.DrugCountry)
	if err != nil {
		return nil, fmt.Errorf("open drug db: %w", err)
	}
	return db, nil
}

func drugINN(d *database.Drug) string {
	if d == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(deref(d.INN)))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
