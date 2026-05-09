package database

// DefaultDaysAfterOpening returns a sensible shelf-life-after-opening value
// for the given medicine `unit`, in days. Returns 0 when the unit has no
// universal default (tablets, capsules, etc. don't degrade meaningfully
// once the box is open).
//
// The numbers come from common pharmacy guidance for general OTC products
// and are intentionally conservative — the user can override per-medicine
// when the package leaflet states a different value. We deliberately don't
// bake in disease-specific exceptions (e.g. opened insulin shelf life)
// because that's where regulatory advice diverges by jurisdiction and the
// app should not pretend to be a clinical reference.
func DefaultDaysAfterOpening(unit string) int {
	switch unit {
	case "drops":
		// Eye/ear drops: typical preservative-loaded multi-dose bottles.
		return 28
	case "ml":
		// Oral suspensions and syrups span 14-28 days; pick the safer end.
		return 14
	case "sachets":
		// Single-dose powders shouldn't be reopened; the count is per
		// sachet, not per box. No "after opening" decay applies — the
		// sachet either is or isn't used.
		return 0
	default:
		// tablets, capsules, mg, g, pieces, patches: no general rule.
		return 0
	}
}
