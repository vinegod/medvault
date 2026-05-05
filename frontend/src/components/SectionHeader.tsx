import type { ReactNode } from "react";

interface Props {
  title: string;
  count?: number;
  // CSS color value (token or literal). Tints the title and count chip
  // so each section reads as its own bucket. Defaults to muted text.
  accentColor?: string;
  // Right-aligned action slot — typically a Button. Renders inline next
  // to the title so the action stays visually coupled to its section.
  action?: ReactNode;
}

// SectionHeader renders an uppercase title with an optional tinted count
// chip and an optional action button on the right. Consolidates the
// pattern that previously lived inline in pages/Alerts.tsx.
export function SectionHeader({ title, count, accentColor, action }: Props) {
  const tint = accentColor ?? "var(--color-text-muted)";
  return (
    <div style={wrap}>
      <h3 style={{ ...heading, color: tint }}>
        <span>{title}</span>
        {typeof count === "number" && (
          <span style={{ ...chip, color: tint, borderColor: tint }}>{count}</span>
        )}
      </h3>
      {action && <div style={actionWrap}>{action}</div>}
    </div>
  );
}

const wrap: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  gap: "0.75rem",
  marginBottom: "0.6rem",
};

const heading: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: "0.5rem",
  fontSize: "0.9rem",
  textTransform: "uppercase",
  letterSpacing: "0.05em",
  margin: 0,
};

const chip: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "center",
  minWidth: 22,
  padding: "0 0.4rem",
  height: 20,
  fontSize: "0.75rem",
  fontWeight: 700,
  border: "1px solid",
  borderRadius: 999,
  background: "transparent",
};

const actionWrap: React.CSSProperties = {
  display: "flex",
  flexShrink: 0,
};
