import { useTranslation } from "react-i18next";
import { Icon } from "./Icon";

interface Props {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}

export function SearchBar({ value, onChange, placeholder }: Props) {
  const { t } = useTranslation();
  return (
    <div style={wrap}>
      <Icon name="search" size={18} style={{ color: "var(--color-text-muted)", flexShrink: 0 }} />
      <input
        style={input}
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder ?? t("search.placeholder")}
        autoComplete="off"
      />
      {value && (
        <button onClick={() => onChange("")} style={clearBtn} aria-label={t("search.clear")}>
          <Icon name="x" size={16} />
        </button>
      )}
    </div>
  );
}

const wrap: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.5rem",
  padding: "0 0.75rem",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  height: 44,
};

const input: React.CSSProperties = {
  flex: 1,
  border: "none",
  outline: "none",
  background: "transparent",
  fontSize: "1rem",
  color: "var(--color-text)",
};

const clearBtn: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  width: 28,
  height: 28,
  borderRadius: "50%",
  background: "var(--color-border)",
  color: "var(--color-text)",
  flexShrink: 0,
};
