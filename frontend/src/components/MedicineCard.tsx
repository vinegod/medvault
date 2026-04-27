import { useState, useEffect, useRef } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "./Icon";
import { updateMedicine } from "../services/db";
import type { LocalMedicine } from "../types";

interface Props {
  medicine: LocalMedicine;
  onAdjust: (delta: number) => void;
  onDelete: () => void;
  lowStockThreshold?: number;
}

export function MedicineCard({ medicine: m, onAdjust, onDelete, lowStockThreshold = 3 }: Props) {
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

  const exp = (() => {
    if (!m.expiration_date) return null;
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const expDate = new Date(m.expiration_date + "T00:00:00");
    const days = Math.round((expDate.getTime() - today.getTime()) / 86400000);
    if (days < 0)
      return { text: t("medicine_card.expired_ago", { days: -days }), color: "var(--color-error)" };
    if (days === 0) return { text: t("medicine_card.expires_today"), color: "var(--color-error)" };
    if (days <= 30)
      return { text: t("medicine_card.expires_in", { days }), color: "var(--color-warning)" };
    if (days <= 90)
      return { text: t("medicine_card.expires_in", { days }), color: "var(--color-text-muted)" };
    return {
      text: t("medicine_card.expires_on", {
        date: expDate.toLocaleDateString(i18n.language, {
          month: "short",
          day: "numeric",
          year: "numeric",
        }),
      }),
      color: "var(--color-text-muted)",
    };
  })();

  return (
    <div style={{ ...card, ...(exp?.color === "var(--color-error)" ? cardExpired : null) }}>
      <Link
        to={`/medicines/${m.id}/edit`}
        style={topRow}
        aria-label={t("medicine_card.edit", { name: m.name })}
      >
        <div style={{ minWidth: 0 }}>
          <div style={nameRow}>
            <strong style={name}>{m.name}</strong>
            {m.sync_status === "pending" && (
              <span style={pendingDot} title={t("medicine_card.not_synced")} />
            )}
          </div>
          {m.category && <div style={muted}>{m.category}</div>}
          {exp && <div style={{ ...exp_, color: exp.color }}>{exp.text}</div>}
          {m.storage_location && <div style={muted}>{m.storage_location}</div>}
        </div>
        <Icon name="edit" size={16} style={{ color: "var(--color-text-muted)", flexShrink: 0 }} />
      </Link>

      <div style={bottomRow}>
        <div style={qtyGroup}>
          <button
            onClick={() => onAdjust(-1)}
            disabled={outOfStock}
            style={{ ...qtyBtn, opacity: outOfStock ? 0.3 : 1 }}
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
const exp_: React.CSSProperties = { fontSize: "0.85rem", marginTop: 2, fontWeight: 500 };
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
  width: 36,
  height: 36,
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
  border: "1px solid var(--color-accent)",
  borderRadius: "var(--radius)",
  padding: "0.1rem 0.25rem",
  outline: "none",
};
const deleteBtn: React.CSSProperties = {
  width: 36,
  height: 36,
  borderRadius: "var(--radius)",
  color: "var(--color-text-muted)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
};
