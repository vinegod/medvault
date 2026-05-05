import { NavLink } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon, type IconName } from "./Icon";

const tabDefs: { to: string; labelKey: string; icon: IconName }[] = [
  { to: "/medicines", labelKey: "nav.medicines", icon: "pill" },
  { to: "/alerts", labelKey: "nav.alerts", icon: "bell" },
  { to: "/settings", labelKey: "nav.settings", icon: "settings" },
];

export function BottomNav() {
  const { t } = useTranslation();
  return (
    <nav style={nav}>
      {tabDefs.map((tab) => (
        <NavLink
          key={tab.to}
          to={tab.to}
          style={({ isActive }) => ({ ...tabStyle, ...(isActive ? tabActive : null) })}
        >
          <Icon name={tab.icon} size={22} />
          <span style={tabLabel}>{t(tab.labelKey)}</span>
        </NavLink>
      ))}
    </nav>
  );
}

const nav: React.CSSProperties = {
  position: "fixed",
  bottom: 0,
  left: 0,
  right: 0,
  height: "calc(var(--nav-h) + var(--safe-bottom))",
  paddingBottom: "var(--safe-bottom)",
  display: "flex",
  background: "var(--color-surface)",
  borderTop: "1px solid var(--color-border)",
  zIndex: 50,
};

const tabStyle: React.CSSProperties = {
  flex: 1,
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  justifyContent: "center",
  gap: "0.2rem",
  color: "var(--color-text-muted)",
  textDecoration: "none",
};

const tabActive: React.CSSProperties = {
  color: "var(--color-primary)",
};

const tabLabel: React.CSSProperties = { fontSize: "0.7rem", fontWeight: 600 };
