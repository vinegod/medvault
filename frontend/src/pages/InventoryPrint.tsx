import { useState } from "react";
import { useLiveQuery } from "dexie-react-hooks";
import { useTranslation } from "react-i18next";
import { getActiveMedicines } from "../services/db";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { effectiveExpiration } from "../utils/effectiveExpiration";

// InventoryPrint shows a printable checklist of medicines, optionally
// limited to one storage location (e.g. a travel kit).
export default function InventoryPrint() {
  const { t, i18n } = useTranslation();
  const medicines = useLiveQuery(() => getActiveMedicines(), []) ?? [];
  const [location, setLocation] = useState("");

  const locations = [
    ...new Set(medicines.map((m) => m.storage_location.trim()).filter(Boolean)),
  ].sort((a, b) => a.localeCompare(b));
  const rows = location
    ? medicines.filter((m) => m.storage_location.trim() === location)
    : medicines;

  return (
    <div>
      <div className="no-print" style={toolbar}>
        <select
          value={location}
          onChange={(e) => setLocation(e.target.value)}
          style={select}
          aria-label={t("filter.location")}
        >
          <option value="">{t("filter.any_location")}</option>
          {locations.map((l) => (
            <option key={l} value={l}>
              {l}
            </option>
          ))}
        </select>
        <Button onClick={() => window.print()} iconLeft={<Icon name="printer" size={16} />}>
          {t("inventory.print")}
        </Button>
      </div>

      <h2 style={{ fontSize: "1.1rem", marginBottom: "0.5rem" }}>
        {t("inventory.title")}
        {location && ` · ${location}`}
        <span style={{ fontWeight: 400, fontSize: "0.85rem", marginLeft: "0.5rem" }}>
          {new Date().toLocaleDateString(i18n.language)}
        </span>
      </h2>

      <table style={table}>
        <thead>
          <tr>
            <th style={th} aria-label={t("inventory.checked")} />
            <th style={th}>{t("medicine_form.name")}</th>
            <th style={th}>{t("medicine_form.active_ingredient")}</th>
            <th style={th}>{t("medicine_form.quantity")}</th>
            <th style={th}>{t("inventory.use_by")}</th>
            <th style={th}>{t("medicine_form.storage_location")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((m) => (
            <tr key={m.id}>
              <td style={td}>☐</td>
              <td style={td}>{m.name}</td>
              <td style={td}>{m.active_ingredient}</td>
              <td style={td}>
                {m.quantity} {t(`units.${m.unit}`)}
              </td>
              <td style={td}>
                {effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening)}
              </td>
              <td style={td}>{m.storage_location}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const toolbar: React.CSSProperties = {
  display: "flex",
  gap: "0.5rem",
  flexWrap: "wrap",
  alignItems: "center",
  marginBottom: "1rem",
};
const select: React.CSSProperties = {
  background: "var(--color-surface)",
  color: "var(--color-text)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.5rem 0.6rem",
  fontSize: "0.9rem",
};
const table: React.CSSProperties = {
  width: "100%",
  borderCollapse: "collapse",
  fontSize: "0.85rem",
};
const th: React.CSSProperties = {
  textAlign: "left",
  borderBottom: "2px solid var(--color-border)",
  padding: "0.4rem",
};
const td: React.CSSProperties = {
  borderBottom: "1px solid var(--color-border)",
  padding: "0.4rem",
};
