import { useLiveQuery } from "dexie-react-hooks";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { getExpiredMedicines, getExpiringMedicines } from "../services/db";
import { EmptyState } from "../components/EmptyState";
import { Icon } from "../components/Icon";
import type { LocalMedicine } from "../types";

function MedicineLink({ m, color }: { m: LocalMedicine; color: string }) {
  const { t } = useTranslation();
  return (
    <Link to={`/medicines/${m.id}/edit`} style={{ ...item, borderLeftColor: color }}>
      <div>
        <strong>{m.name}</strong>
        <div style={{ fontSize: "0.8rem", color: "var(--color-text-muted)" }}>
          {m.quantity} {t(`units.${m.unit}`)} · {m.expiration_date}
        </div>
      </div>
      <Icon name="edit" size={16} style={{ color: "var(--color-text-muted)" }} />
    </Link>
  );
}

function Section({ title, color, items }: { title: string; color: string; items: LocalMedicine[] }) {
  if (!items.length) return null;
  return (
    <div style={{ marginBottom: "1.5rem" }}>
      <h3 style={{ color, marginBottom: "0.6rem", fontSize: "0.9rem", textTransform: "uppercase", letterSpacing: "0.05em" }}>
        {title} · {items.length}
      </h3>
      <div style={{ display: "flex", flexDirection: "column", gap: "0.5rem" }}>
        {items.map((m) => <MedicineLink key={m.id} m={m} color={color} />)}
      </div>
    </div>
  );
}

export default function Alerts() {
  const { t } = useTranslation();
  const expired = useLiveQuery(() => getExpiredMedicines(), []) ?? [];
  const expiring30 = useLiveQuery(() => getExpiringMedicines(30), []) ?? [];
  const expiring90 = useLiveQuery(() => getExpiringMedicines(90), []) ?? [];

  const today = new Date().toISOString().split("T")[0];
  const soon = expiring30.filter((m) => (m.expiration_date ?? "") >= today);
  const upcoming = expiring90.filter((m) => !expiring30.find((e) => e.id === m.id));

  const total = expired.length + soon.length + upcoming.length;

  if (total === 0) {
    return (
      <EmptyState
        icon="check"
        title={t("alerts.all_clear_title")}
        description={t("alerts.all_clear_description")}
      />
    );
  }

  return (
    <div>
      <Section title={t("alerts.expired")}     color="var(--color-error)"      items={expired} />
      <Section title={t("alerts.expiring_30")} color="var(--color-warning)"    items={soon} />
      <Section title={t("alerts.expiring_90")} color="var(--color-text-muted)" items={upcoming} />
    </div>
  );
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
