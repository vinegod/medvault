import { useTranslation } from "react-i18next";
import { useAuth } from "../contexts/AuthContext";
import { Icon } from "./Icon";

export function SyncBadge() {
  const { status } = useAuth();
  const { t } = useTranslation();

  const cfg = (() => {
    switch (status) {
      case "connected":  return { icon: "wifi" as const,     color: "var(--color-success)",    label: t("sync.synced") };
      case "connecting": return { icon: "wifi" as const,     color: "var(--color-warning)",    label: t("sync.connecting") };
      case "error":      return { icon: "wifi-off" as const, color: "var(--color-error)",      label: t("sync.reconnect") };
      default:           return { icon: "wifi-off" as const, color: "var(--color-text-muted)", label: t("sync.local") };
    }
  })();

  return (
    <span title={cfg.label} style={{ display: "inline-flex", alignItems: "center", gap: "0.35rem", color: cfg.color, fontSize: "0.8rem" }}>
      <Icon name={cfg.icon} size={16} />
      {cfg.label}
    </span>
  );
}
