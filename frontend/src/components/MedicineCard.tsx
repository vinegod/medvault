import { useState, useEffect, useRef } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "./Icon";
import { updateMedicine } from "../services/db";
import { fetchPhoto } from "../services/photos";
import { effectiveExpiration } from "../utils/effectiveExpiration";
import type { LocalMedicine } from "../types";
import { DEFAULT_LOW_STOCK } from "../constants";

// disambiguator returns a short subtitle to distinguish two medicines
// that share a name and have no category. Order: expiration → added-date
// → trailing id chars. Storage location is intentionally skipped — it
// already renders on its own line below.
function disambiguator(m: LocalMedicine, lang: string): string {
  if (m.expiration_date) {
    const d = new Date(m.expiration_date + "T00:00:00Z");
    return `exp ${d.toLocaleDateString(lang, { month: "short", year: "numeric", timeZone: "UTC" })}`;
  }
  if (m.created_at) {
    const d = new Date(m.created_at);
    return `added ${d.toLocaleDateString(lang, { month: "short", day: "numeric" })}`;
  }
  return `id …${m.id.slice(-4)}`;
}

interface Props {
  medicine: LocalMedicine;
  onAdjust: (delta: number) => void;
  onDelete: () => void;
  lowStockThreshold?: number;
  // True when another visible medicine shares this one's name. The card
  // surfaces a fallback subtitle (storage / expiry / added-date / id-tail)
  // so the duplicates aren't visually identical when category is empty.
  isAmbiguous?: boolean;
  // Wall-clock reference passed in by the parent so render stays pure.
  // Used for the "Restock?" pill staleness check. Defaults to 0, which
  // disables the pill — callers that want it must opt in.
  now?: number;
}

export function MedicineCard({
  medicine: m,
  onAdjust,
  onDelete,
  lowStockThreshold = DEFAULT_LOW_STOCK,
  isAmbiguous = false,
  now = 0,
}: Props) {
  const { t, i18n } = useTranslation();
  const lowStock = m.quantity > 0 && m.quantity <= lowStockThreshold;
  const outOfStock = m.quantity === 0;

  // Inline-edit state for the quantity number. Tap the number → input;
  // Enter or blur commits, Escape cancels. Keeps the +/- buttons for
  // small changes while letting users type 30 directly when refilling.
  const [editingQty, setEditingQty] = useState(false);
  const [qtyDraft, setQtyDraft] = useState(String(m.quantity));
  const qtyInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (editingQty) qtyInputRef.current?.select();
  }, [editingQty]);

  function startEditQty() {
    setQtyDraft(String(m.quantity));
    setEditingQty(true);
  }

  function commitQty() {
    const parsed = Math.max(0, Math.min(99999, Number.parseInt(qtyDraft, 10) || 0));
    setEditingQty(false);
    if (parsed !== m.quantity) updateMedicine(m.id, { quantity: parsed });
  }

  // Photo thumbnail. The cache stores the URL alongside the hash it was
  // fetched for; the displayed `thumbUrl` is derived from cache + current
  // prop. That means clearing photo_hash doesn't require a synchronous
  // setState-in-effect — it just stops matching the cache key.
  const [photoCache, setPhotoCache] = useState<{ hash: string; url: string } | null>(null);
  useEffect(() => {
    if (!m.photo_hash) return;
    let url: string | null = null;
    let cancelled = false;
    fetchPhoto(m.id, m.photo_hash).then((u) => {
      if (cancelled) {
        if (u) URL.revokeObjectURL(u);
        return;
      }
      if (u) {
        url = u;
        setPhotoCache({ hash: m.photo_hash!, url: u });
      }
    });
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [m.id, m.photo_hash]);
  const thumbUrl = photoCache && photoCache.hash === m.photo_hash ? photoCache.url : null;

  // Use the effective expiration (printed-date and post-opening shelf life
  // combined) so opened liquids surface as expiring before the box date.
  const effectiveExp = effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening);
  const exp = (() => {
    if (!effectiveExp) return null;
    // Compute the day delta in UTC so the bucket doesn't flip when the
    // user crosses midnight local time. effectiveExp is "YYYY-MM-DD".
    const [ey, em, ed] = effectiveExp.split("-").map(Number);
    const expUTC = Date.UTC(ey, em - 1, ed);
    const todayLocal = new Date();
    const todayUTC = Date.UTC(
      todayLocal.getFullYear(),
      todayLocal.getMonth(),
      todayLocal.getDate()
    );
    const days = Math.round((expUTC - todayUTC) / 86400000);

    if (days < 0)
      return { text: t("medicine_card.expired_ago", { days: -days }), color: "var(--color-error)" };
    if (days === 0) return { text: t("medicine_card.expires_today"), color: "var(--color-error)" };
    if (days <= 30)
      return { text: t("medicine_card.expires_in", { days }), color: "var(--color-warning)" };
    if (days <= 90)
      return { text: t("medicine_card.expires_in", { days }), color: "var(--color-text-muted)" };
    return {
      text: t("medicine_card.expires_on", {
        date: new Date(expUTC).toLocaleDateString(i18n.language, {
          month: "short",
          day: "numeric",
          year: "numeric",
          timeZone: "UTC",
        }),
      }),
      color: "var(--color-text-muted)",
    };
  })();

  // Severity stripe at the card's left edge — red for "act now"
  // (out-of-stock or expired), amber for "act soon" (low-stock or
  // expiring within 30 days), nothing otherwise. Mirrors the
  // colour-coded quantity / expiration text but readable at a glance.
  const isExpired = exp?.color === "var(--color-error)";
  const isExpiringSoon = exp?.color === "var(--color-warning)";
  const stripeColor = (() => {
    if (outOfStock || isExpired) return "var(--color-destructive)";
    if (lowStock || isExpiringSoon) return "var(--color-warning)";
    return null;
  })();

  // "Restock?" pill — only when out of stock for more than 30 days.
  // Encourages action on long-stale rows without nagging on fresh zeros.
  // The wall-clock reference comes from the `now` prop so render stays
  // pure (react-hooks/purity disallows direct Date.now() reads here).
  const showRestockPill = (() => {
    if (!outOfStock || !now) return false;
    const updatedMs = new Date(m.updated_at).getTime();
    if (Number.isNaN(updatedMs)) return false;
    return now - updatedMs > 30 * 24 * 60 * 60 * 1000;
  })();

  return (
    <div
      style={{
        ...card,
        ...(isExpired ? cardExpired : null),
        ...(stripeColor ? { borderLeft: `4px solid ${stripeColor}` } : null),
      }}
    >
      <Link
        to={`/medicines/${m.id}/edit`}
        style={topRow}
        aria-label={t("medicine_card.edit", { name: m.name })}
      >
        {thumbUrl && (
          <img
            src={thumbUrl}
            alt=""
            style={thumb}
            // Decorative — the link's aria-label already announces the medicine.
            aria-hidden="true"
          />
        )}
        <div style={{ minWidth: 0, flex: 1 }}>
          <div style={nameRow}>
            <strong style={name}>{m.name}</strong>
            {m.sync_status === "pending" && (
              <span style={pendingDot} title={t("medicine_card.not_synced")} />
            )}
            {showRestockPill && <span style={restockPill}>{t("medicine_card.restock")}</span>}
          </div>
          {m.category ? (
            <div style={muted}>{m.category}</div>
          ) : (
            isAmbiguous && <div style={muted}>{disambiguator(m, i18n.language)}</div>
          )}
          {exp && <div style={{ ...exp_, color: exp.color }}>{exp.text}</div>}
          {m.storage_location && <div style={muted}>{m.storage_location}</div>}
        </div>
      </Link>

      <div style={bottomRow}>
        <div style={qtyGroup}>
          <button
            onClick={() => onAdjust(-1)}
            disabled={outOfStock}
            aria-disabled={outOfStock || undefined}
            style={{
              ...qtyBtn,
              opacity: outOfStock ? 0.45 : 1,
              cursor: outOfStock ? "not-allowed" : "pointer",
            }}
            aria-label={t("medicine_card.decrement")}
          >
            <Icon name="minus" size={18} />
          </button>
          <div
            style={{
              ...qtyDisplay,
              color: outOfStock
                ? "var(--color-error)"
                : lowStock
                  ? "var(--color-warning)"
                  : "var(--color-text)",
            }}
          >
            {editingQty ? (
              <input
                ref={qtyInputRef}
                type="number"
                inputMode="numeric"
                min={0}
                max={99999}
                value={qtyDraft}
                onChange={(e) => setQtyDraft(e.target.value)}
                onBlur={commitQty}
                onKeyDown={(e) => {
                  if (e.key === "Enter") commitQty();
                  if (e.key === "Escape") setEditingQty(false);
                }}
                style={qtyInput}
                aria-label={t("medicine_card.set_quantity")}
              />
            ) : (
              <button
                onClick={startEditQty}
                style={qtyValueBtn}
                aria-label={t("medicine_card.set_quantity")}
              >
                {m.quantity}
              </button>
            )}
            <span style={{ fontSize: "0.75rem", color: "var(--color-text-muted)" }}>
              {t(`units.${m.unit}`)}
            </span>
          </div>
          <button
            onClick={() => onAdjust(+1)}
            style={qtyBtn}
            aria-label={t("medicine_card.increment")}
          >
            <Icon name="plus" size={18} />
          </button>
        </div>

        <button
          onClick={onDelete}
          style={deleteBtn}
          aria-label={t("medicine_card.delete", { name: m.name })}
        >
          <Icon name="trash" size={18} />
        </button>
      </div>
    </div>
  );
}

const card: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius-lg)",
  padding: "0.75rem 1rem",
  display: "flex",
  flexDirection: "column",
  gap: "0.5rem",
};
const cardExpired: React.CSSProperties = { borderColor: "rgba(233,69,96,.4)" };
const topRow: React.CSSProperties = {
  display: "flex",
  alignItems: "flex-start",
  justifyContent: "space-between",
  gap: "0.75rem",
  color: "inherit",
  textDecoration: "none",
  padding: "0.25rem 0",
  minHeight: 44,
};
const nameRow: React.CSSProperties = { display: "flex", alignItems: "center", gap: "0.5rem" };
const name: React.CSSProperties = {
  fontSize: "1.05rem",
  fontWeight: 600,
  whiteSpace: "nowrap",
  overflow: "hidden",
  textOverflow: "ellipsis",
  maxWidth: "100%",
};
const muted: React.CSSProperties = {
  fontSize: "0.8rem",
  color: "var(--color-text-muted)",
  marginTop: 2,
};
const restockPill: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  background: "var(--color-warning)",
  color: "#1a1a2e",
  fontSize: "0.65rem",
  fontWeight: 700,
  textTransform: "uppercase",
  letterSpacing: "0.05em",
  padding: "0.1rem 0.4rem",
  borderRadius: 999,
};
const exp_: React.CSSProperties = { fontSize: "0.85rem", marginTop: 2, fontWeight: 500 };
const thumb: React.CSSProperties = {
  width: 48,
  height: 48,
  objectFit: "cover",
  borderRadius: "var(--radius)",
  border: "1px solid var(--color-border)",
  flexShrink: 0,
};
const pendingDot: React.CSSProperties = {
  width: 8,
  height: 8,
  borderRadius: "50%",
  background: "var(--color-warning)",
  flexShrink: 0,
};
const bottomRow: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  gap: "0.5rem",
  borderTop: "1px solid var(--color-border)",
  paddingTop: "0.5rem",
};
const qtyGroup: React.CSSProperties = { display: "flex", alignItems: "center", gap: "0.5rem" };
const qtyBtn: React.CSSProperties = {
  width: "var(--tap)",
  height: "var(--tap)",
  borderRadius: "50%",
  background: "var(--color-surface-2)",
  color: "var(--color-text)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  border: "1px solid var(--color-border)",
};
const qtyDisplay: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  minWidth: 56,
  lineHeight: 1.1,
};
const qtyValueBtn: React.CSSProperties = {
  fontSize: "1.1rem",
  fontWeight: 700,
  color: "inherit",
  background: "transparent",
  border: "none",
  padding: "0 0.25rem",
  cursor: "pointer",
};
const qtyInput: React.CSSProperties = {
  width: 64,
  fontSize: "1.05rem",
  fontWeight: 700,
  textAlign: "center",
  color: "inherit",
  background: "var(--color-bg)",
  border: "1px solid var(--color-primary)",
  borderRadius: "var(--radius)",
  padding: "0.1rem 0.25rem",
  outline: "none",
};
const deleteBtn: React.CSSProperties = {
  width: "var(--tap)",
  height: "var(--tap)",
  borderRadius: "var(--radius)",
  color: "var(--color-text-muted)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
};
