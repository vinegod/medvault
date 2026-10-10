import { useTranslation } from "react-i18next";
import type { AuditEntry } from "../types";

interface Props {
  entries: AuditEntry[];
}

// AuditList renders activity-log rows: who did what to which target, with
// a quantity change when the row carries one.
export function AuditList({ entries }: Props) {
  const { t } = useTranslation();

  if (entries.length === 0) {
    return (
      <p style={{ color: "var(--color-text-muted)", fontSize: "0.85rem" }}>
        {t("household.activity_empty")}
      </p>
    );
  }

  return (
    <ul style={{ listStyle: "none" }}>
      {entries.map((e) => (
        <li key={e.id} style={row}>
          <div>
            <strong>{e.username}</strong>{" "}
            <span style={{ color: "var(--color-text-muted)" }}>
              {t(`household.audit.${e.action}`, {
                defaultValue: e.action,
                target: e.target_name ?? "",
              })}
            </span>
            {e.quantity_before !== null && e.quantity_after !== null && (
              <span style={{ marginLeft: "0.4rem", fontWeight: 600 }}>
                {e.quantity_before} → {e.quantity_after}
              </span>
            )}
          </div>
          <span style={time}>{new Date(e.at).toLocaleString()}</span>
        </li>
      ))}
    </ul>
  );
}

const row: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  gap: "0.5rem",
  padding: "0.5rem 0",
  borderBottom: "1px solid var(--color-border)",
  fontSize: "0.85rem",
};

const time: React.CSSProperties = {
  fontSize: "0.75rem",
  color: "var(--color-text-muted)",
  whiteSpace: "nowrap",
};
