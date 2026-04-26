import { useLocation, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "./Icon";
import { SyncBadge } from "./SyncBadge";

export function Header() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { t } = useTranslation();

  const titles: Record<string, { title: string; back?: boolean }> = {
    "/medicines":      { title: t("header.medicines") },
    "/medicines/new":  { title: t("header.add_medicine"), back: true },
    "/alerts":         { title: t("header.alerts") },
    "/settings":       { title: t("header.settings") },
    "/household":      { title: t("header.household"), back: true },
    "/household/join": { title: t("header.join_household"), back: true },
  };

  const entry = titles[pathname] ?? (
    pathname.startsWith("/medicines/") && pathname.endsWith("/edit")
      ? { title: t("header.edit_medicine"), back: true }
      : { title: t("header.fallback") }
  );

  return (
    <header style={header}>
      <div style={{ display: "flex", alignItems: "center", gap: "0.5rem", minWidth: 0, flex: 1 }}>
        {entry.back && (
          <button onClick={() => navigate(-1)} style={backBtn} aria-label={t("header.go_back")}>
            <Icon name="back" size={22} />
          </button>
        )}
        <h1 style={titleStyle}>{entry.title}</h1>
      </div>
      <SyncBadge />
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
  width: 40,
  height: 40,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  borderRadius: "var(--radius)",
  color: "var(--color-text-muted)",
  marginLeft: "-0.5rem",
};
