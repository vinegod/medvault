import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "./Icon";
import { MedicineCard } from "./MedicineCard";
import { expiryBucket } from "../utils/effectiveExpiration";
import type { MedicineGroup } from "../utils/groups";

interface Props {
  group: MedicineGroup;
  onAdjust: (id: string, delta: number) => void;
  onDelete: (id: string, name: string) => void;
  lowStockThreshold: number;
  today: string;
  now: number;
}

// MedicinePackGroup is a collapsible card for several packs of the same
// medicine: a summary header, then one MedicineCard per pack when expanded.
export function MedicinePackGroup({
  group,
  onAdjust,
  onDelete,
  lowStockThreshold,
  today,
  now,
}: Props) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const first = group.packs[0];
  const bucket = group.earliestExpiration
    ? expiryBucket(
        { expiration_date: group.earliestExpiration, opened_at: null, days_after_opening: null },
        today
      )
    : "none";
  const expColor =
    bucket === "expired"
      ? "var(--color-error)"
      : bucket === "soon"
        ? "var(--color-warning)"
        : "var(--color-text-muted)";

  return (
    <div style={{ ...card, gridColumn: open ? "1 / -1" : undefined }}>
      <button onClick={() => setOpen((v) => !v)} style={header} aria-expanded={open}>
        <div style={{ minWidth: 0, flex: 1, textAlign: "left" }}>
          <strong style={name}>{first.name}</strong>
          <div style={muted}>
            {t("packs.summary", {
              count: group.packs.length,
              total: group.totalQuantity,
              unit: t(`units.${first.unit}`),
            })}
          </div>
          {group.earliestExpiration && (
            <div style={{ ...muted, color: expColor, fontWeight: 500 }}>
              {t("packs.earliest", {
                date: new Date(group.earliestExpiration + "T00:00:00Z").toLocaleDateString(
                  i18n.language,
                  {
                    month: "short",
                    day: "numeric",
                    year: "numeric",
                    timeZone: "UTC",
                  }
                ),
              })}
            </div>
          )}
        </div>
        <Icon
          name="chevron-down"
          size={18}
          style={{ transform: open ? "rotate(180deg)" : "none", transition: "transform 120ms" }}
        />
      </button>

      {open && (
        <>
          <div className="grid-cards-lg">
            {group.packs.map((m) => (
              <MedicineCard
                key={m.id}
                medicine={m}
                onAdjust={(delta) => onAdjust(m.id, delta)}
                onDelete={() => onDelete(m.id, m.name)}
                lowStockThreshold={lowStockThreshold}
                isAmbiguous
                now={now}
              />
            ))}
          </div>
          <Link to={`/medicines/new?copy=${first.id}`} style={addLink}>
            <Icon name="plus" size={16} />
            {t("packs.add")}
          </Link>
        </>
      )}
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
  gap: "0.75rem",
};
const header: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.75rem",
  width: "100%",
  background: "transparent",
  border: "none",
  color: "var(--color-text)",
  padding: "0.25rem 0",
  minHeight: 44,
  cursor: "pointer",
};
const name: React.CSSProperties = { fontSize: "1.05rem", fontWeight: 600 };
const muted: React.CSSProperties = {
  fontSize: "0.8rem",
  color: "var(--color-text-muted)",
  marginTop: 2,
};
const addLink: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: "0.4rem",
  color: "var(--color-primary)",
  fontWeight: 600,
  fontSize: "0.9rem",
  textDecoration: "none",
  alignSelf: "flex-start",
};
