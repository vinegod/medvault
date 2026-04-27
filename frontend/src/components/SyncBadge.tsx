import { useState } from "react";
import { useLiveQuery } from "dexie-react-hooks";
import { useTranslation } from "react-i18next";
import { useAuth } from "../contexts/AuthContext";
import { db } from "../services/db";
import { performSync } from "../services/sync";
import { Icon } from "./Icon";

// SyncBadge is the small sync-status pill in the header. When connected
// it doubles as a manual "sync now" button and shows how long ago the
// last successful sync ran (re-renders live via dexie-react-hooks).
export function SyncBadge() {
  const { status } = useAuth();
  const { t, i18n } = useTranslation();
  const [busy, setBusy] = useState(false);

  // Reactive read of the last_sync meta key so the relative time updates
  // automatically after each successful sync.
  const lastSync = useLiveQuery(async () => {
    const r = await db.meta.get("last_sync");
    return r?.value ?? null;
  }, []);

  const cfg = (() => {
    switch (status) {
      case "connected":
        return { icon: "wifi" as const, color: "var(--color-success)", label: t("sync.synced") };
      case "connecting":
        return {
          icon: "wifi" as const,
          color: "var(--color-warning)",
          label: t("sync.connecting"),
        };
      case "error":
        return {
          icon: "wifi-off" as const,
          color: "var(--color-error)",
          label: t("sync.reconnect"),
        };
      default:
        return {
          icon: "wifi-off" as const,
          color: "var(--color-text-muted)",
          label: t("sync.local"),
        };
    }
  })();

  async function handleClick() {
    if (status !== "connected" || busy) return;
    setBusy(true);
    try {
      await performSync();
    } finally {
      setBusy(false);
    }
  }

  const relative =
    status === "connected" ? formatRelative(lastSync ?? null, i18n.language, t) : null;
  const tooltip = relative ? `${cfg.label} · ${relative}` : cfg.label;

  // Connected: render as a button. Other states stay non-interactive.
  if (status === "connected") {
    return (
      <button
        onClick={handleClick}
        disabled={busy}
        title={tooltip}
        aria-label={t("sync.sync_now")}
        style={{ ...style, color: cfg.color, cursor: busy ? "default" : "pointer" }}
      >
        <Icon name={cfg.icon} size={16} />
        <span>{busy ? t("sync.syncing") : (relative ?? cfg.label)}</span>
      </button>
    );
  }

  return (
    <span title={cfg.label} style={{ ...style, color: cfg.color }}>
      <Icon name={cfg.icon} size={16} />
      {cfg.label}
    </span>
  );
}

// formatRelative returns "Synced X min ago" / "Synced just now" etc.
// Falls back to "never" when last_sync hasn't been set yet (the user just
// connected and the first sync hasn't finished).
function formatRelative(
  iso: string | null,
  _lang: string,
  t: (k: string, opts?: Record<string, unknown>) => string
): string {
  if (!iso) return t("sync.never");
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 0) return t("sync.just_now");
  const sec = Math.round(ms / 1000);
  if (sec < 30) return t("sync.just_now");
  if (sec < 60) return t("sync.seconds_ago", { count: sec });
  const min = Math.round(sec / 60);
  if (min < 60) return t("sync.minutes_ago", { count: min });
  const hr = Math.round(min / 60);
  if (hr < 24) return t("sync.hours_ago", { count: hr });
  const day = Math.round(hr / 24);
  return t("sync.days_ago", { count: day });
}

const style: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: "0.35rem",
  fontSize: "0.8rem",
  background: "transparent",
  border: "none",
  padding: "0.25rem 0.5rem",
  borderRadius: "var(--radius)",
};
