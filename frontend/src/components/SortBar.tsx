import { useTranslation } from "react-i18next";

export type SortKey = "name" | "quantity" | "expiration" | "recent";

const KEYS: SortKey[] = ["name", "quantity", "expiration", "recent"];

export function isSortKey(v: string): v is SortKey {
  return (KEYS as string[]).includes(v);
}

interface Props {
  value: SortKey;
  onChange: (key: SortKey) => void;
}

// SortBar is a horizontal pill-button strip for picking the medicines list
// sort order. Compact enough to live in the page chrome above the list.
export function SortBar({ value, onChange }: Props) {
  const { t } = useTranslation();
  return (
    <div style={bar} role="radiogroup" aria-label={t("sort.label")}>
      {KEYS.map((k) => {
        const active = k === value;
        return (
          <button
            key={k}
            role="radio"
            aria-checked={active}
            onClick={() => onChange(k)}
            style={{ ...pill, ...(active ? pillActive : null) }}
          >
            {t(`sort.${k}`)}
          </button>
        );
      })}
    </div>
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
  background: "var(--color-accent)",
  color: "#fff",
  borderColor: "var(--color-accent)",
};
