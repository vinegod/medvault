import type { ReactNode } from "react";
import { useLiveQuery } from "dexie-react-hooks";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { db, getActiveMedicines, getLowStockMedicines } from "../services/db";
import { EmptyState } from "../components/EmptyState";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { SectionHeader } from "../components/SectionHeader";
import { useToast } from "../contexts/ToastContext";
import { effectiveExpiration } from "../utils/effectiveExpiration";
import type { LocalMedicine } from "../types";
import { DEFAULT_LOW_STOCK } from "../constants";

function MedicineLink({
  m,
  color,
  showQuantity,
}: {
  m: LocalMedicine;
  color: string;
  showQuantity?: boolean;
}) {
  const { t, i18n } = useTranslation();
  // Low-stock rows lead with quantity; expiration rows lead with the date.
  // Localize the date the same way MedicineCard does so the two views agree.
  const effectiveExp = effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening);
  const subtitle = showQuantity
    ? `${m.quantity} ${t(`units.${m.unit}`)}`
    : `${m.quantity} ${t(`units.${m.unit}`)} · ${formatExpirationDate(effectiveExp, i18n.language)}`;
  return (
    <Link to={`/medicines/${m.id}/edit`} style={{ ...item, borderLeftColor: color }}>
      <div>
        <strong>{m.name}</strong>
        <div style={{ fontSize: "0.8rem", color: "var(--color-text-muted)" }}>{subtitle}</div>
      </div>
      <Icon name="edit" size={16} style={{ color: "var(--color-text-muted)" }} />
    </Link>
  );
}

function Section({
  title,
  color,
  items,
  showQuantity,
  action,
}: {
  title: string;
  color: string;
  items: LocalMedicine[];
  showQuantity?: boolean;
  action?: ReactNode;
}) {
  if (!items.length) return null;
  return (
    <div style={{ marginBottom: "1.5rem" }}>
      <SectionHeader title={title} count={items.length} accentColor={color} action={action} />
      <div style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
        {items.map((m) => (
          <MedicineLink key={m.id} m={m} color={color} showQuantity={showQuantity} />
        ))}
      </div>
    </div>
  );
}

export default function Alerts() {
  const { t, i18n } = useTranslation();
  const toast = useToast();
  const threshold =
    useLiveQuery(async () => {
      const r = await db.meta.get("low_stock_threshold");
      const n = r?.value ? Number.parseInt(r.value, 10) : DEFAULT_LOW_STOCK;
      return Number.isNaN(n) ? DEFAULT_LOW_STOCK : n;
    }, []) ?? DEFAULT_LOW_STOCK;
  // Source of truth: every active medicine. We bucket client-side using
  // effective expiration (printed-date + post-opening shelf life), which
  // means an opened bottle of eye drops can land in "expired" before its
  // box date — exactly what the feature is for.
  const all = useLiveQuery(() => getActiveMedicines(), []) ?? [];
  const lowStock = useLiveQuery(() => getLowStockMedicines(threshold), [threshold]) ?? [];

  const today = new Date().toISOString().split("T")[0];
  const expired: LocalMedicine[] = [];
  const soon: LocalMedicine[] = [];
  const upcoming: LocalMedicine[] = [];
  for (const m of all) {
    const eff = effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening);
    if (!eff) continue;
    if (eff < today) expired.push(m);
    else {
      const days = daysUntil(eff, today);
      if (days <= 30) soon.push(m);
      else if (days <= 90) upcoming.push(m);
    }
  }

  const total = expired.length + soon.length + upcoming.length + lowStock.length;

  async function handleExportShoppingList() {
    const lines = buildShoppingList(t, i18n.language, expired, soon, lowStock);
    const text = lines.join("\n");
    // Web Share API on supporting platforms (mobile, often), clipboard
    // everywhere else. Either way the user ends up with the list to paste.
    if (typeof navigator !== "undefined" && "share" in navigator) {
      try {
        await navigator.share({ text, title: t("alerts.shopping_list_title") });
        return;
      } catch (err) {
        if (err instanceof Error && err.name === "AbortError") return;
        // fall through to clipboard
      }
    }
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t("alerts.shopping_list_copied"));
    } catch {
      toast.error(t("alerts.shopping_list_failed"));
    }
  }

  if (total === 0) {
    return (
      <EmptyState
        icon="check"
        title={t("alerts.all_clear_title")}
        description={t("alerts.all_clear_description")}
      />
    );
  }

  // The Export button rides on the section that motivates it most: the
  // user's most actionable bucket. Prefer low-stock (the literal "shopping
  // list"), fall back to expired, then to expiring-soon — whichever is
  // present first carries the action so the page doesn't ever omit it
  // when there's something to export.
  const exportButton = (
    <Button
      variant="secondary"
      size="sm"
      onClick={handleExportShoppingList}
      iconLeft={<Icon name="upload" size={16} />}
    >
      {t("alerts.export_shopping_list")}
    </Button>
  );
  let exportConsumed = false;
  function takeExport(): ReactNode | undefined {
    if (exportConsumed) return undefined;
    exportConsumed = true;
    return exportButton;
  }

  return (
    <div>
      <Section
        title={t("alerts.low_stock")}
        color="var(--color-warning)"
        items={lowStock}
        showQuantity
        action={lowStock.length ? takeExport() : undefined}
      />
      <Section
        title={t("alerts.expired")}
        color="var(--color-error)"
        items={expired}
        action={expired.length ? takeExport() : undefined}
      />
      <Section
        title={t("alerts.expiring_30")}
        color="var(--color-warning)"
        items={soon}
        action={soon.length ? takeExport() : undefined}
      />
      <Section title={t("alerts.expiring_90")} color="var(--color-text-muted)" items={upcoming} />
    </div>
  );
}

// buildShoppingList produces the plaintext lines that go into the user's
// clipboard or share sheet. Headed with a date so the list stays useful
// when sent to oneself / dropped into a notes app.
function buildShoppingList(
  t: (k: string, opts?: Record<string, unknown>) => string,
  lang: string,
  expired: LocalMedicine[],
  soon: LocalMedicine[],
  lowStock: LocalMedicine[]
): string[] {
  const lines: string[] = [];
  lines.push(t("alerts.shopping_list_title"));
  lines.push(`(${new Date().toLocaleDateString(lang)})`);
  lines.push("");

  if (expired.length > 0 || soon.length > 0) {
    lines.push(t("alerts.shopping_list_replace"));
    for (const m of expired) lines.push(`  - ${m.name}`);
    for (const m of soon) lines.push(`  - ${m.name}`);
    lines.push("");
  }
  if (lowStock.length > 0) {
    lines.push(t("alerts.shopping_list_restock"));
    for (const m of lowStock) {
      lines.push(`  - ${m.name} (${m.quantity} ${t(`units.${m.unit}`)})`);
    }
  }
  return lines;
}

// formatExpirationDate renders an ISO YYYY-MM-DD as a short localized date,
// anchoring at UTC midnight so the displayed day matches what the user typed
// regardless of their timezone.
function formatExpirationDate(iso: string | null, lang: string): string {
  if (!iso) return "";
  const [y, mo, d] = iso.split("-").map(Number);
  return new Date(Date.UTC(y, mo - 1, d)).toLocaleDateString(lang, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}

// daysUntil returns the integer days between two YYYY-MM-DD strings, treating
// both as UTC midnight. Used to bucket within-90-days into within-30-days.
function daysUntil(target: string, from: string): number {
  if (!target) return Number.MAX_SAFE_INTEGER;
  const [ty, tm, td] = target.split("-").map(Number);
  const [fy, fm, fd] = from.split("-").map(Number);
  return Math.round((Date.UTC(ty, tm - 1, td) - Date.UTC(fy, fm - 1, fd)) / 86400000);
}

const item: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderLeft: "3px solid",
  borderRadius: "var(--radius)",
  padding: "0.75rem 1rem",
  textDecoration: "none",
  color: "var(--color-text)",
};
