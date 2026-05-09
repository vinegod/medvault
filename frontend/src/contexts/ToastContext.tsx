import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { Icon } from "../components/Icon";

type ToastType = "success" | "error" | "info";

interface Toast {
  id: number;
  type: ToastType;
  message: string;
  actionLabel?: string;
  onAction?: () => void;
}

// Optional action shown as a button on the right of a toast. Used by
// Medicines.tsx to offer "Undo" right after a delete.
interface ToastOptions {
  ms?: number;
  actionLabel?: string;
  onAction?: () => void;
}

interface ToastContextValue {
  show: (type: ToastType, message: string, opts?: ToastOptions) => void;
  success: (message: string, opts?: ToastOptions) => void;
  error: (message: string, opts?: ToastOptions) => void;
  info: (message: string, opts?: ToastOptions) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const dismiss = useCallback((id: number) => {
    setToasts((t) => t.filter((x) => x.id !== id));
  }, []);

  const show = useCallback(
    (type: ToastType, message: string, opts: ToastOptions = {}) => {
      const id = ++nextId;
      const ms = opts.ms ?? (type === "error" ? 5000 : 3000);
      setToasts((t) => [
        ...t,
        { id, type, message, actionLabel: opts.actionLabel, onAction: opts.onAction },
      ]);
      setTimeout(() => dismiss(id), ms);
    },
    [dismiss]
  );

  const value: ToastContextValue = {
    show,
    success: (m, o) => show("success", m, o),
    error: (m, o) => show("error", m, o),
    info: (m, o) => show("info", m, o),
  };

  function handleAction(toast: Toast) {
    toast.onAction?.();
    dismiss(toast.id);
  }

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div style={container} aria-live="polite" aria-atomic="true">
        {toasts.map((t) => (
          <div key={t.id} style={{ ...toastBox, ...toastStyles[t.type] }}>
            <button
              style={toastBody}
              onClick={() => dismiss(t.id)}
              aria-label="Dismiss notification"
            >
              <Icon name={iconFor(t.type)} size={18} />
              <span>{t.message}</span>
            </button>
            {t.actionLabel && t.onAction && (
              <button style={actionBtn} onClick={() => handleAction(t)}>
                {t.actionLabel}
              </button>
            )}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used inside <ToastProvider>");
  return ctx;
}

function iconFor(t: ToastType) {
  if (t === "success") return "check" as const;
  if (t === "error") return "warning" as const;
  return "bell" as const;
}

const container: React.CSSProperties = {
  position: "fixed",
  left: "50%",
  bottom: "calc(var(--nav-h) + var(--safe-bottom) + 16px)",
  transform: "translateX(-50%)",
  display: "flex",
  flexDirection: "column",
  gap: "0.5rem",
  zIndex: 1000,
  pointerEvents: "none",
  width: "min(calc(100% - 32px), 360px)",
};

const toastBox: React.CSSProperties = {
  display: "flex",
  alignItems: "stretch",
  gap: "0",
  borderRadius: "var(--radius)",
  fontSize: "0.9rem",
  border: "1px solid",
  pointerEvents: "auto",
  width: "100%",
  boxShadow: "0 8px 24px rgba(0,0,0,.4)",
  animation: "toast-in 180ms ease-out",
  overflow: "hidden",
};

const toastBody: React.CSSProperties = {
  flex: 1,
  display: "flex",
  alignItems: "center",
  gap: "0.6rem",
  padding: "0.75rem 1rem",
  background: "transparent",
  border: "none",
  color: "inherit",
  textAlign: "left",
  cursor: "pointer",
  font: "inherit",
};

const actionBtn: React.CSSProperties = {
  padding: "0 1rem",
  background: "rgba(255, 255, 255, 0.1)",
  border: "none",
  borderLeft: "1px solid rgba(255, 255, 255, 0.15)",
  color: "inherit",
  fontWeight: 600,
  cursor: "pointer",
  fontSize: "0.85rem",
  textTransform: "uppercase",
  letterSpacing: "0.05em",
};

const toastStyles: Record<ToastType, React.CSSProperties> = {
  success: { background: "#1a3a2a", color: "#a8efc6", borderColor: "#2c5b42" },
  error: { background: "#3a1a22", color: "#ffc4cf", borderColor: "#7a2c3c" },
  info: {
    background: "var(--color-surface-2)",
    color: "var(--color-text)",
    borderColor: "var(--color-border)",
  },
};
