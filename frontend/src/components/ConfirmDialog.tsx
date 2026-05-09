import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "./Button";

interface Props {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel,
  destructive = false,
  onConfirm,
  onCancel,
}: Props) {
  const { t } = useTranslation();
  if (!open) return null;

  return (
    <div style={backdrop} onClick={onCancel} role="dialog" aria-modal="true" aria-label={title}>
      <div style={card} onClick={(e) => e.stopPropagation()}>
        <h3 style={titleStyle}>{title}</h3>
        {description && <div style={descStyle}>{description}</div>}
        <div style={actions}>
          <Button variant="secondary" onClick={onCancel} fullWidth>
            {cancelLabel ?? t("confirm_dialog.cancel")}
          </Button>
          <Button
            variant={destructive ? "danger" : "primary"}
            onClick={onConfirm}
            fullWidth
            autoFocus
          >
            {confirmLabel ?? t("confirm_dialog.confirm")}
          </Button>
        </div>
      </div>
    </div>
  );
}

const backdrop: React.CSSProperties = {
  position: "fixed",
  inset: 0,
  background: "rgba(0,0,0,0.55)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  padding: "1rem",
  zIndex: 1000,
};

const card: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius-lg)",
  padding: "1.5rem",
  width: "100%",
  maxWidth: 400,
};

const titleStyle: React.CSSProperties = {
  fontSize: "1.1rem",
  marginBottom: "0.75rem",
};

const descStyle: React.CSSProperties = {
  color: "var(--color-text-muted)",
  fontSize: "0.95rem",
  marginBottom: "1.25rem",
  lineHeight: 1.5,
};

const actions: React.CSSProperties = {
  display: "flex",
  gap: "0.5rem",
};
