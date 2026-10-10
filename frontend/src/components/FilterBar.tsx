import { useTranslation } from "react-i18next";
import { expiryBucket } from "../utils/effectiveExpiration";
import type { LocalMedicine } from "../types";

export type StatusFilter = "all" | "expired" | "expiring" | "low" | "opened";

const STATUSES: StatusFilter[] = ["all", "expired", "expiring", "low", "opened"];

export interface MedicineFilter {
  status: StatusFilter;
  category: string; // "" = any
  location: string; // "" = any
}

export const NO_FILTER: MedicineFilter = { status: "all", category: "", location: "" };

// parseFilter restores a saved filter, falling back to NO_FILTER for
// missing or malformed values.
export function parseFilter(raw: string | null): MedicineFilter {
  if (!raw) return NO_FILTER;
  try {
    const f = JSON.parse(raw) as Partial<MedicineFilter>;
    return {
      status: STATUSES.includes(f.status as StatusFilter) ? (f.status as StatusFilter) : "all",
      category: typeof f.category === "string" ? f.category : "",
      location: typeof f.location === "string" ? f.location : "",
    };
  } catch {
    return NO_FILTER;
  }
}

// matchesFilter reports whether m passes every part of f.
export function matchesFilter(
  m: LocalMedicine,
  f: MedicineFilter,
  lowStockThreshold: number,
  today: string
): boolean {
  if (f.category && m.category !== f.category) return false;
  if (f.location && m.storage_location !== f.location) return false;
  switch (f.status) {
    case "expired":
      return expiryBucket(m, today) === "expired";
    case "expiring":
      return expiryBucket(m, today) === "soon";
    case "low":
      return m.quantity <= lowStockThreshold;
    case "opened":
      return m.opened_at !== null && m.opened_at !== "";
    default:
      return true;
  }
}

interface Props {
  value: MedicineFilter;
  onChange: (f: MedicineFilter) => void;
  medicines: LocalMedicine[];
}

// FilterBar shows status chips plus category and location selects. A
// select appears only when there are at least two values to choose from.
export function FilterBar({ value, onChange, medicines }: Props) {
  const { t } = useTranslation();
  const categories = distinct(medicines.map((m) => m.category));
  const locations = distinct(medicines.map((m) => m.storage_location));
  const active = value.status !== "all" || value.category !== "" || value.location !== "";

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
      <div style={bar} role="radiogroup" aria-label={t("filter.label")}>
        {STATUSES.map((s) => {
          const on = s === value.status;
          return (
            <button
              key={s}
              role="radio"
              aria-checked={on}
              onClick={() => onChange({ ...value, status: s })}
              style={{ ...pill, ...(on ? pillActive : null) }}
            >
              {t(`filter.${s}`)}
            </button>
          );
        })}
      </div>
      {(categories.length > 1 || locations.length > 1 || active) && (
        <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap", alignItems: "center" }}>
          {categories.length > 1 && (
            <select
              style={select}
              value={value.category}
              onChange={(e) => onChange({ ...value, category: e.target.value })}
              aria-label={t("filter.category")}
            >
              <option value="">{t("filter.any_category")}</option>
              {categories.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          )}
          {locations.length > 1 && (
            <select
              style={select}
              value={value.location}
              onChange={(e) => onChange({ ...value, location: e.target.value })}
              aria-label={t("filter.location")}
            >
              <option value="">{t("filter.any_location")}</option>
              {locations.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
          )}
          {active && (
            <button onClick={() => onChange(NO_FILTER)} style={clearBtn}>
              {t("filter.clear")}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function distinct(values: string[]): string[] {
  return [...new Set(values.map((v) => v.trim()).filter(Boolean))].sort((a, b) =>
    a.localeCompare(b)
  );
}

const bar: React.CSSProperties = {
  display: "flex",
  gap: "0.4rem",
  overflowX: "auto",
  scrollbarWidth: "none",
};

const pill: React.CSSProperties = {
  flex: "0 0 auto",
  padding: "0.4rem 0.85rem",
  borderRadius: 999,
  background: "var(--color-surface)",
  color: "var(--color-text-muted)",
  border: "1px solid var(--color-border)",
  fontSize: "0.8rem",
  fontWeight: 600,
  cursor: "pointer",
};

const pillActive: React.CSSProperties = {
  background: "var(--color-primary)",
  color: "#fff",
  borderColor: "var(--color-primary)",
};

const select: React.CSSProperties = {
  background: "var(--color-surface)",
  color: "var(--color-text)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.4rem 0.6rem",
  fontSize: "0.85rem",
  maxWidth: "100%",
};

const clearBtn: React.CSSProperties = {
  background: "transparent",
  border: "none",
  color: "var(--color-primary)",
  fontSize: "0.85rem",
  fontWeight: 600,
  cursor: "pointer",
  padding: "0.4rem 0.25rem",
};
