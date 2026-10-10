// Computes the effective expiration date of a medicine, combining the
// printed expiration with the post-opening shelf life. A bottle of eye
// drops printed "exp 2028" is unsafe past 28 days after opening, regardless
// of what the box says.
//
// Returns the earlier of:
//   - expiration_date (if set)
//   - opened_at + days_after_opening (if both set)
//
// All inputs and outputs are "YYYY-MM-DD" strings; computation is anchored
// at UTC midnight so a user crossing local-time midnight doesn't flip the
// bucket. Returns null when the medicine has no expiration tracking at all.

const MS_PER_DAY = 86_400_000;

export function effectiveExpiration(
  expirationDate: string | null,
  openedAt: string | null,
  daysAfterOpening: number | null
): string | null {
  const printed = expirationDate ?? null;
  const opened = openedAt && daysAfterOpening ? openedAt : null;

  if (!printed && !opened) return null;
  if (printed && !opened) return printed;
  if (!printed && opened) return addDays(opened!, daysAfterOpening!);

  // Both set — pick the earlier date.
  const openedExp = addDays(opened!, daysAfterOpening!);
  return printed! < openedExp ? printed! : openedExp;
}

// Days from "today" until the given YYYY-MM-DD, anchored at UTC midnight.
// Negative when the date is in the past.
export function daysUntil(target: string): number {
  const [ty, tm, td] = target.split("-").map(Number);
  const targetUTC = Date.UTC(ty, tm - 1, td);
  const now = new Date();
  const todayUTC = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
  return Math.round((targetUTC - todayUTC) / MS_PER_DAY);
}

export type ExpiryBucket = "expired" | "soon" | "upcoming" | "ok" | "none";

// expiryBucket classifies a medicine's effective expiration against today
// (YYYY-MM-DD): past, within 30 days, within 90 days, later, or untracked.
export function expiryBucket(
  m: {
    expiration_date: string | null;
    opened_at: string | null;
    days_after_opening: number | null;
  },
  today: string
): ExpiryBucket {
  const eff = effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening);
  if (!eff) return "none";
  if (eff < today) return "expired";
  const days = Math.round((utcDay(eff) - utcDay(today)) / MS_PER_DAY);
  if (days <= 30) return "soon";
  if (days <= 90) return "upcoming";
  return "ok";
}

function utcDay(iso: string): number {
  const [y, m, d] = iso.split("-").map(Number);
  return Date.UTC(y, m - 1, d);
}

// Default shelf-life-after-opening for a given unit, in days. Mirrors
// backend/internal/database/shelflife.go DefaultDaysAfterOpening — keep the
// two in lockstep. Returns null when no universal default applies.
export function defaultDaysAfterOpening(unit: string): number | null {
  switch (unit) {
    case "drops":
      return 28;
    case "ml":
      return 14;
    default:
      return null;
  }
}

// addDays returns the YYYY-MM-DD string that's `days` days after `iso`.
function addDays(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const t = Date.UTC(y, m - 1, d) + days * MS_PER_DAY;
  const dt = new Date(t);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${dt.getUTCFullYear()}-${pad(dt.getUTCMonth() + 1)}-${pad(dt.getUTCDate())}`;
}
