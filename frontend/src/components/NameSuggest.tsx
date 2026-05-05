import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ApiResponse, DrugSuggestion } from "../types";

// joinDrugNotes is the canonical formatter for the medicine `notes` field
// when filled from a drug suggestion. Mirrors backend/internal/database/
// drugdb.go JoinDrugNotes — keep the two in lockstep.
export function joinDrugNotes(inn?: string | null, pharmaGroup?: string | null): string {
  const parts: string[] = [];
  if (inn && inn.trim()) parts.push(inn.trim());
  if (pharmaGroup && pharmaGroup.trim()) parts.push(pharmaGroup.trim());
  return parts.join("\n");
}

interface Props {
  value: string;
  onChange: (v: string) => void;
  onPick: (s: DrugSuggestion) => void;
  placeholder?: string;
  required?: boolean;
  autoFocus?: boolean;
  maxLength?: number;
  inputStyle?: React.CSSProperties;
}

const DEBOUNCE_MS = 200;
const MIN_CHARS = 2;
const MAX_RESULTS = 8;
const EXPANDED_RESULTS = 25;

export function NameSuggest(props: Props) {
  const { t } = useTranslation();
  const [suggestions, setSuggestions] = useState<DrugSuggestion[]>([]);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const [activeIdx, setActiveIdx] = useState(-1);
  // expandedQuery tracks the query string for which the user clicked "Show more".
  // expanded is derived: true only when current value still matches.
  const [expandedQuery, setExpandedQuery] = useState<string | null>(null);
  const expanded = expandedQuery !== null && expandedQuery === props.value.trim();
  const blurTimer = useRef<number | undefined>(undefined);
  const reqId = useRef(0);

  useEffect(() => {
    const q = props.value.trim();
    const ready = q.length >= MIN_CHARS;
    const limit = expanded ? EXPANDED_RESULTS : MAX_RESULTS;
    const handle = window.setTimeout(
      async () => {
        if (!ready) {
          reqId.current++;
          setSuggestions([]);
          setActiveIdx(-1);
          setLoading(false);
          return;
        }
        const myId = ++reqId.current;
        setLoading(true);
        try {
          const res = await fetch(`/api/drugs/search?q=${encodeURIComponent(q)}&limit=${limit}`);
          const body = (await res.json()) as ApiResponse<DrugSuggestion[]>;
          if (myId !== reqId.current) return;
          setSuggestions(body.data ?? []);
          setActiveIdx(-1);
        } catch {
          if (myId !== reqId.current) return;
          setSuggestions([]);
          setActiveIdx(-1);
        } finally {
          if (myId === reqId.current) setLoading(false);
        }
      },
      ready ? DEBOUNCE_MS : 0
    );
    return () => window.clearTimeout(handle);
  }, [props.value, expanded]);

  function handleFocus() {
    if (blurTimer.current !== undefined) {
      window.clearTimeout(blurTimer.current);
      blurTimer.current = undefined;
    }
    setOpen(true);
  }

  function handleBlur() {
    blurTimer.current = window.setTimeout(() => {
      setOpen(false);
      setActiveIdx(-1);
    }, 150);
  }

  function pick(s: DrugSuggestion) {
    if (blurTimer.current !== undefined) {
      window.clearTimeout(blurTimer.current);
      blurTimer.current = undefined;
    }
    setOpen(false);
    setActiveIdx(-1);
    props.onPick(s);
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Escape") {
      setOpen(false);
      setActiveIdx(-1);
      return;
    }
    if (!open || suggestions.length === 0) return;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => Math.min(i + 1, suggestions.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" && activeIdx >= 0) {
      e.preventDefault();
      pick(suggestions[activeIdx]);
    }
  }

  const ready = props.value.trim().length >= MIN_CHARS;
  const showDropdown = open && ready;

  return (
    <div style={{ position: "relative" }}>
      <input
        style={props.inputStyle}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
        onFocus={handleFocus}
        onBlur={handleBlur}
        onKeyDown={handleKeyDown}
        placeholder={props.placeholder}
        required={props.required}
        autoFocus={props.autoFocus}
        maxLength={props.maxLength}
        autoComplete="off"
        role="combobox"
        aria-expanded={showDropdown}
        aria-haspopup="listbox"
        aria-autocomplete="list"
      />
      {showDropdown && (
        <ul style={dropdown} role="listbox">
          {loading ? (
            <li style={statusRow}>{t("medicine_form.suggestions_loading")}</li>
          ) : suggestions.length === 0 ? (
            <li style={statusRow}>{t("medicine_form.suggestions_none")}</li>
          ) : (
            <>
              {suggestions.map((s, idx) => (
                <li
                  key={`${s.country}|${s.name}`}
                  style={{ listStyle: "none" }}
                  role="option"
                  aria-selected={idx === activeIdx}
                >
                  <button
                    type="button"
                    style={{
                      ...row,
                      background: idx === activeIdx ? "var(--color-surface-2)" : "transparent",
                    }}
                    onMouseDown={(e) => e.preventDefault()}
                    onMouseEnter={() => setActiveIdx(idx)}
                    onClick={() => pick(s)}
                  >
                    <div style={rowMain}>
                      <strong style={rowName}>{s.name}</strong>
                      {s.inn && <span style={rowInn}>{s.inn}</span>}
                    </div>
                    {s.pharma_group && <span style={rowPharma}>{s.pharma_group}</span>}
                  </button>
                </li>
              ))}
              {!expanded && suggestions.length === MAX_RESULTS && (
                <li style={{ listStyle: "none" }}>
                  <button
                    type="button"
                    style={{ ...row, justifyContent: "center", color: "var(--color-primary)" }}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => setExpandedQuery(props.value.trim())}
                  >
                    {t("medicine_form.show_more")}
                  </button>
                </li>
              )}
            </>
          )}
        </ul>
      )}
    </div>
  );
}

const dropdown: React.CSSProperties = {
  position: "absolute",
  top: "100%",
  left: 0,
  right: 0,
  zIndex: 100,
  margin: "4px 0 0 0",
  padding: 0,
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  maxHeight: 320,
  overflowY: "auto",
  boxShadow: "0 4px 12px rgba(0, 0, 0, 0.18)",
};

const statusRow: React.CSSProperties = {
  listStyle: "none",
  padding: "0.6rem 0.75rem",
  color: "var(--color-text-muted)",
  fontSize: "0.9rem",
};

const row: React.CSSProperties = {
  display: "flex",
  width: "100%",
  alignItems: "flex-start",
  justifyContent: "space-between",
  gap: "0.75rem",
  padding: "0.6rem 0.75rem",
  color: "var(--color-text)",
  textAlign: "left",
  borderBottom: "1px solid var(--color-border)",
  cursor: "pointer",
};

const rowMain: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  flex: 1,
  minWidth: 0,
};

const rowName: React.CSSProperties = {
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

const rowInn: React.CSSProperties = {
  fontSize: "0.85rem",
  color: "var(--color-text-muted)",
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

const rowPharma: React.CSSProperties = {
  fontSize: "0.75rem",
  color: "var(--color-text-muted)",
  maxWidth: "40%",
  textAlign: "right",
};
