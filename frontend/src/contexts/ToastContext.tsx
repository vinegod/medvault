import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { Icon } from "../components/Icon";

type ToastType = "success" | "error" | "info";

interface Toast {
  id: number;
  type: ToastType;
  message: string;
}

interface ToastContextValue {
  show: (type: ToastType, message: string, ms?: number) => void;
  success: (message: string) => void;
  error: (message: string) => void;
  info: (message: string) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const dismiss = useCallback((id: number) => {
    setToasts((t) => t.filter((x) => x.id !== id));
  }, []);

  const show = useCallback((type: ToastType, message: string, ms = 3000) => {
    const id = ++nextId;
    setToasts((t) => [...t, { id, type, message }]);
    setTimeout(() => dismiss(id), ms);
  }, [dismiss]);

  const value: ToastContextValue = {
    show,
    success: (m) => show("success", m),
    error: (m) => show("error", m, 5000),
    info: (m) => show("info", m),
  };

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div style={container} aria-live="polite" aria-atomic="true">
        {toasts.map((t) => (
          <button
            key={t.id}
            style={{ ...toast, ...toastStyles[t.type] }}
            onClick={() => dismiss(t.id)}
          >
            <Icon name={iconFor(t.type)} size={18} />
            <span>{t.message}</span>
          </button>
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

const toast: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.6rem",
  padding: "0.75rem 1rem",
  borderRadius: "var(--radius)",
  fontSize: "0.9rem",
  border: "1px solid",
  pointerEvents: "auto",
  cursor: "pointer",
  textAlign: "left",
  width: "100%",
  boxShadow: "0 8px 24px rgba(0,0,0,.4)",
  animation: "toast-in 180ms ease-out",
};

const toastStyles: Record<ToastType, React.CSSProperties> = {
  success: { background: "#1a3a2a", color: "#a8efc6", borderColor: "#2c5b42" },
  error:   { background: "#3a1a22", color: "#ffc4cf", borderColor: "#7a2c3c" },
  info:    { background: "var(--color-surface-2)", color: "var(--color-text)", borderColor: "var(--color-border)" },
};
