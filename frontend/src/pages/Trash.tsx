import { useLiveQuery } from "dexie-react-hooks";
import { useTranslation } from "react-i18next";
import { getDeletedMedicines, restoreMedicine } from "../services/db";
import { EmptyState } from "../components/EmptyState";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { Skeleton } from "../components/Skeleton";
import { useToast } from "../contexts/ToastContext";

// Trash lists deleted and disposed medicines and lets the user restore them.
export default function Trash() {
  const { t, i18n } = useTranslation();
  const toast = useToast();
  const deleted = useLiveQuery(() => getDeletedMedicines(), []);

  async function handleRestore(id: string, name: string) {
    await restoreMedicine(id);
    toast.success(t("trash.restored", { name }));
  }

  if (deleted === undefined) return <Skeleton count={3} height={64} />;
  if (deleted.length === 0) {
    return (
      <EmptyState
        icon="trash"
        title={t("trash.empty_title")}
        description={t("trash.empty_description")}
      />
    );
  }

  return (
    <ul style={{ listStyle: "none", display: "flex", flexDirection: "column", gap: "0.5rem" }}>
      {deleted.map((m) => (
        <li key={m.id} style={row}>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
              <strong>{m.name}</strong>
              {m.disposed_at && <span style={badge}>{t("trash.disposed")}</span>}
            </div>
            <div style={{ fontSize: "0.8rem", color: "var(--color-text-muted)" }}>
              {t("trash.deleted_on", {
                date: new Date(m.deleted_at!).toLocaleDateString(i18n.language),
              })}
              {" · "}
              {m.quantity} {t(`units.${m.unit}`)}
            </div>
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => handleRestore(m.id, m.name)}
            iconLeft={<Icon name="restore" size={14} />}
          >
            {t("trash.restore")}
          </Button>
        </li>
      ))}
    </ul>
  );
}

const row: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.75rem",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.75rem 1rem",
};

const badge: React.CSSProperties = {
  background: "var(--color-surface-2)",
  color: "var(--color-text-muted)",
  border: "1px solid var(--color-border)",
  fontSize: "0.7rem",
  fontWeight: 600,
  padding: "0.05rem 0.4rem",
  borderRadius: 999,
};
