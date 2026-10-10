import { useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "./Icon";
import { SyncBadge } from "./SyncBadge";
import { HouseholdSwitcherSheet } from "./HouseholdSwitcherSheet";
import { useAuth } from "../contexts/AuthContext";

export function Header() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { t } = useTranslation();
  const { households } = useAuth();
  const [switcherOpen, setSwitcherOpen] = useState(false);

  const titles: Record<string, { title: string; back?: boolean }> = {
    "/medicines": { title: t("header.medicines") },
    "/medicines/new": { title: t("header.add_medicine"), back: true },
    "/alerts": { title: t("header.alerts") },
    "/settings": { title: t("header.settings") },
    "/household": { title: t("header.household"), back: true },
    "/household/new": { title: t("header.create_household"), back: true },
    "/household/join": { title: t("header.join_household"), back: true },
    "/trash": { title: t("header.trash"), back: true },
    "/inventory/print": { title: t("header.inventory"), back: true },
    "/onboarding/household": { title: t("header.fallback") },
  };

  const entry =
    titles[pathname] ??
    (pathname.startsWith("/medicines/") && pathname.endsWith("/edit")
      ? { title: t("header.edit_medicine"), back: true }
      : { title: t("header.fallback") });

  // Show the switcher button only on root pages (no back button), when the
  // user belongs to 2+ households. Avoids cluttering the header on pages
  // that already use the back affordance.
  const showSwitcher = !entry.back && households.length >= 2;
  const activeHousehold = households.find((h) => h.is_active);

  return (
    <header className="no-print" style={header}>
      <div style={{ display: "flex", alignItems: "center", gap: "0.5rem", minWidth: 0, flex: 1 }}>
        {entry.back && (
          <button onClick={() => navigate(-1)} style={backBtn} aria-label={t("header.go_back")}>
            <Icon name="back" size={22} />
          </button>
        )}
        <h1 style={titleStyle}>{entry.title}</h1>
        {showSwitcher && activeHousehold && (
          <button
            onClick={() => setSwitcherOpen(true)}
            style={switcherBtn}
            aria-label={t("household_switcher.open")}
            title={activeHousehold.name}
          >
            <span style={switcherName}>{activeHousehold.name}</span>
            <Icon name="chevron-down" size={14} />
          </button>
        )}
      </div>
      <SyncBadge />

      <HouseholdSwitcherSheet open={switcherOpen} onClose={() => setSwitcherOpen(false)} />
    </header>
  );
}

const header: React.CSSProperties = {
  position: "sticky",
  top: 0,
  zIndex: 40,
  height: "calc(var(--header-h) + var(--safe-top))",
  paddingTop: "var(--safe-top)",
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  gap: "1rem",
  padding: "var(--safe-top) 1rem 0 1rem",
  background: "var(--color-bg)",
  borderBottom: "1px solid var(--color-border)",
};

const titleStyle: React.CSSProperties = {
  fontSize: "1.1rem",
  fontWeight: 600,
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
};

const backBtn: React.CSSProperties = {
  width: "var(--tap)",
  height: "var(--tap)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  borderRadius: "var(--radius)",
  color: "var(--color-text-muted)",
  marginLeft: "-0.5rem",
};

const switcherBtn: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.25rem",
  padding: "0.25rem 0.5rem",
  background: "var(--color-surface-2)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  color: "var(--color-text)",
  fontSize: "0.8rem",
  cursor: "pointer",
  maxWidth: 160,
  minHeight: 32,
};

const switcherName: React.CSSProperties = {
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
};
