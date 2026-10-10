import { Link } from "react-router-dom";
import type { ReactNode } from "react";

interface Props {
  to: string;
  label: string;
  children: ReactNode;
}

// Floating Action Button: bottom-right, above the BottomNav, in the
// thumb zone for one-handed phone use. Reserved for the page's primary
// action ("Add medicine"). Never put more than one FAB on a screen.
export function FAB({ to, label, children }: Props) {
  return (
    <Link to={to} aria-label={label} style={style} className="no-print">
      {children}
    </Link>
  );
}

const style: React.CSSProperties = {
  position: "fixed",
  right: 20,
  bottom: "calc(var(--nav-h) + var(--safe-bottom) + 16px)",
  width: 56,
  height: 56,
  borderRadius: "50%",
  background: "var(--color-primary)",
  color: "#fff",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  boxShadow: "0 8px 24px rgba(233,69,96,.4)",
  zIndex: 30,
  textDecoration: "none",
};
