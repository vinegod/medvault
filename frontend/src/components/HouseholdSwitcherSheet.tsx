import { useEffect } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { Icon } from "./Icon";

interface Props {
  open: boolean;
  onClose: () => void;
}

// HouseholdSwitcherSheet is a bottom-sheet listing every household the
// caller belongs to. Tapping a non-active row calls switchHousehold;
// the bottom of the sheet links to Manage / Create / Join flows.
export function HouseholdSwitcherSheet({ open, onClose }: Props) {
  const { t } = useTranslation();
  const { households, switchHousehold } = useAuth();
  const toast = useToast();

  // Close on Escape so keyboard users aren't trapped.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  async function handleSelect(id: string, isActive: boolean) {
    if (isActive) {
      onClose();
      return;
    }
    onClose();
    const ok = await switchHousehold(id);
    if (!ok) toast.error(t("household_switcher.switch_failed"));
  }

  return (
    // Close only when the click started AND ended on the backdrop itself.
    // Without this guard, a drag-select that begins outside the sheet and
    // releases on the backdrop closes the sheet — surprising on touch and
    // when copying text.
    <div
      style={backdrop}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
      role="presentation"
    >
      <div style={sheet} role="dialog" aria-modal="true" aria-label={t("household_switcher.title")}>
        <div style={handle} />
        <h2 style={title}>{t("household_switcher.title")}</h2>

        <ul style={list}>
          {households.map((h) => (
            <li key={h.id}>
              <button
                onClick={() => handleSelect(h.id, h.is_active)}
                style={{ ...row, ...(h.is_active ? rowActive : null) }}
              >
                <Icon name="home" size={18} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={rowName}>{h.name}</div>
                  <div style={rowMeta}>
                    {h.role === "owner"
                      ? t("household_switcher.owner")
                      : t("household_switcher.member")}
                  </div>
                </div>
                {h.is_active && (
                  <Icon name="check" size={18} style={{ color: "var(--color-primary)" }} />
                )}
              </button>
            </li>
          ))}
        </ul>

        <div style={actions}>
          <Link to="/household" onClick={onClose} style={actionLink}>
            <Icon name="settings" size={16} />
            <span>{t("household_switcher.manage")}</span>
          </Link>
          <Link to="/household/new" onClick={onClose} style={actionLink}>
            <Icon name="plus" size={16} />
            <span>{t("household_switcher.create")}</span>
          </Link>
          <Link to="/household/join" onClick={onClose} style={actionLink}>
            <Icon name="users" size={16} />
            <span>{t("household_switcher.join")}</span>
          </Link>
        </div>
      </div>
    </div>
  );
}

const backdrop: React.CSSProperties = {
  position: "fixed",
  inset: 0,
  background: "rgba(0, 0, 0, 0.5)",
  zIndex: 100,
  display: "flex",
  alignItems: "flex-end",
  justifyContent: "center",
};
const sheet: React.CSSProperties = {
  width: "100%",
  maxWidth: 520,
  background: "var(--color-surface)",
  borderTopLeftRadius: "var(--radius-lg)",
  borderTopRightRadius: "var(--radius-lg)",
  padding: "0.5rem 1rem calc(1rem + var(--safe-bottom))",
  display: "flex",
  flexDirection: "column",
};
const handle: React.CSSProperties = {
  width: 40,
  height: 4,
  background: "var(--color-border)",
  borderRadius: 2,
  margin: "0.5rem auto 0.75rem",
};
const title: React.CSSProperties = {
  fontSize: "1rem",
  fontWeight: 600,
  marginBottom: "0.5rem",
};
const list: React.CSSProperties = {
  listStyle: "none",
  display: "flex",
  flexDirection: "column",
  gap: "0.4rem",
  margin: "0.25rem 0 0.75rem",
};
const row: React.CSSProperties = {
  width: "100%",
  display: "flex",
  alignItems: "center",
  gap: "0.75rem",
  padding: "0.75rem",
  background: "var(--color-surface-2)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  color: "var(--color-text)",
  textAlign: "left",
  cursor: "pointer",
};
const rowActive: React.CSSProperties = {
  borderColor: "var(--color-primary)",
};
const rowName: React.CSSProperties = {
  fontWeight: 600,
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
};
const rowMeta: React.CSSProperties = {
  fontSize: "0.75rem",
  color: "var(--color-text-muted)",
  textTransform: "capitalize",
};
const actions: React.CSSProperties = {
  borderTop: "1px solid var(--color-border)",
  paddingTop: "0.75rem",
  display: "flex",
  flexDirection: "column",
  gap: "0.25rem",
};
const actionLink: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.6rem",
  padding: "0.65rem 0.5rem",
  color: "var(--color-text)",
  textDecoration: "none",
  borderRadius: "var(--radius)",
};
