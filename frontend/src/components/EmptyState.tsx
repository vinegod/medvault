import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";

interface Props {
  icon: IconName;
  title: string;
  description?: string;
  action?: ReactNode;
}

// Empty states should always answer "what should I do next?" — every
// blank screen is a chance to teach the user the next action.
export function EmptyState({ icon, title, description, action }: Props) {
  return (
    <div style={wrap}>
      <div style={iconCircle}>
        <Icon name={icon} size={32} />
      </div>
      <h3 style={titleStyle}>{title}</h3>
      {description && <p style={descStyle}>{description}</p>}
      {action && <div style={{ marginTop: "1rem" }}>{action}</div>}
    </div>
  );
}

const wrap: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  textAlign: "center",
  padding: "3rem 1rem 2rem",
  gap: "0.5rem",
};

const iconCircle: React.CSSProperties = {
  width: 72,
  height: 72,
  borderRadius: "50%",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  color: "var(--color-text-muted)",
  marginBottom: "0.5rem",
};

const titleStyle: React.CSSProperties = { fontSize: "1.1rem", fontWeight: 600 };
const descStyle: React.CSSProperties = {
  color: "var(--color-text-muted)",
  fontSize: "0.95rem",
  maxWidth: 320,
};
