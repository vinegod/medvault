import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";

type Variant = "primary" | "secondary" | "danger" | "ghost";
type Size = "sm" | "md" | "lg";

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  iconLeft?: ReactNode;
  iconRight?: ReactNode;
  fullWidth?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, Props>(function Button(
  { variant = "primary", size = "md", iconLeft, iconRight, fullWidth, style, children, ...rest },
  ref
) {
  return (
    <button
      ref={ref}
      style={{
        ...base,
        ...sizes[size],
        ...variants[variant],
        ...(fullWidth && { width: "100%" }),
        ...style,
      }}
      {...rest}
    >
      {iconLeft}
      {children}
      {iconRight}
    </button>
  );
});

const base: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "center",
  gap: "0.5rem",
  borderRadius: "var(--radius)",
  fontWeight: 600,
  border: "1px solid transparent",
  transition: "background 120ms, opacity 120ms, transform 80ms",
  userSelect: "none",
};

const sizes: Record<Size, React.CSSProperties> = {
  sm: { fontSize: "0.85rem", padding: "0.4rem 0.75rem", minHeight: 36 },
  md: { fontSize: "0.95rem", padding: "0.625rem 1.1rem", minHeight: 44 },
  lg: { fontSize: "1rem", padding: "0.85rem 1.4rem", minHeight: 52 },
};

const variants: Record<Variant, React.CSSProperties> = {
  primary: { background: "var(--color-accent)", color: "#fff" },
  secondary: {
    background: "var(--color-surface-2)",
    color: "var(--color-text)",
    borderColor: "var(--color-border)",
  },
  danger: {
    background: "transparent",
    color: "var(--color-error)",
    borderColor: "var(--color-error)",
  },
  ghost: { background: "transparent", color: "var(--color-text-muted)" },
};
