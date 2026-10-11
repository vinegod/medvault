import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useLiveQuery } from "dexie-react-hooks";
import { aiChat } from "../services/ai";
import { getActiveMedicines } from "../services/db";
import { performSync } from "../services/sync";
import { useAiStatus } from "../hooks/useAiStatus";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { EmptyState } from "../components/EmptyState";
import type { ChatMessage } from "../types";

interface Turn extends ChatMessage {
  warning?: string;
  medicineIds?: string[];
  failed?: boolean;
}

const MAX_HISTORY = 20;

export default function Assistant() {
  const { t } = useTranslation();
  const ai = useAiStatus();
  const [turns, setTurns] = useState<Turn[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const synced = useRef(false);
  const endRef = useRef<HTMLDivElement | null>(null);
  const medicines = useLiveQuery(() => getActiveMedicines(), []);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [turns, busy]);

  async function send(e: FormEvent) {
    e.preventDefault();
    const text = input.trim();
    if (!text || busy) return;

    const next: Turn[] = [...turns, { role: "user", content: text }];
    setTurns(next);
    setInput("");
    setBusy(true);

    if (!synced.current) {
      synced.current = true;
      try {
        await performSync();
      } catch {
        // The chat still works on the last synced inventory.
      }
    }

    const history: ChatMessage[] = next
      .filter((m) => !m.failed)
      .slice(-MAX_HISTORY)
      .map(({ role, content }) => ({ role, content }));
    const res = await aiChat(history);
    setBusy(false);

    if (!res.data) {
      setTurns([
        ...next,
        { role: "assistant", content: res.error?.message ?? t("ai.chat_failed"), failed: true },
      ]);
      return;
    }
    setTurns([
      ...next,
      {
        role: "assistant",
        content: res.data.text,
        warning: res.data.warning,
        medicineIds: res.data.medicine_ids,
      },
    ]);
  }

  if (!ai.enabled) {
    return (
      <EmptyState
        icon="sparkles"
        title={t("ai.disabled_title")}
        description={t("ai.disabled_description")}
      />
    );
  }

  const byId = new Map((medicines ?? []).map((m) => [m.id, m]));

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
      <p style={disclaimer}>
        {t("ai.disclaimer")}
        {!ai.local && ` ${t("ai.remote_note")}`}
      </p>

      {turns.length === 0 && <p style={hint}>{t("ai.chat_hint")}</p>}

      {turns.map((m, i) => (
        <div key={i} style={m.role === "user" ? userBubble : assistantBubble}>
          {m.warning && (
            <div role="alert" style={warningBox}>
              <Icon name="warning" size={16} />
              <span>{m.warning}</span>
            </div>
          )}
          <div
            style={{ whiteSpace: "pre-wrap", color: m.failed ? "var(--color-error)" : undefined }}
          >
            {m.content}
          </div>
          {m.medicineIds && m.medicineIds.length > 0 && (
            <div style={{ display: "flex", flexWrap: "wrap", gap: "0.35rem", marginTop: "0.5rem" }}>
              {m.medicineIds.map((id) => {
                const med = byId.get(id);
                if (!med) return null;
                return (
                  <Link key={id} to={`/medicines/${id}/edit`} style={chip}>
                    <Icon name="pill" size={14} />
                    {med.name} · {med.quantity} {t(`units.${med.unit}`)}
                  </Link>
                );
              })}
            </div>
          )}
        </div>
      ))}

      {busy && <p style={hint}>{t("ai.thinking")}</p>}
      <div ref={endRef} />

      <form onSubmit={send} style={composer}>
        <input
          style={composerInput}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={t("ai.chat_placeholder")}
          maxLength={4000}
          disabled={busy}
          aria-label={t("ai.chat_placeholder")}
        />
        <Button
          type="submit"
          variant="primary"
          disabled={busy || !input.trim()}
          aria-label={t("ai.send")}
          iconLeft={<Icon name="send" size={16} />}
        >
          {t("ai.send")}
        </Button>
      </form>
    </div>
  );
}

const disclaimer: React.CSSProperties = {
  fontSize: "0.8rem",
  color: "var(--color-text-muted)",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.6rem 0.75rem",
};
const hint: React.CSSProperties = { color: "var(--color-text-muted)", fontSize: "0.9rem" };
const bubble: React.CSSProperties = {
  maxWidth: "88%",
  padding: "0.65rem 0.85rem",
  borderRadius: "var(--radius-lg)",
  fontSize: "0.95rem",
  lineHeight: 1.45,
};
const userBubble: React.CSSProperties = {
  ...bubble,
  alignSelf: "flex-end",
  background: "var(--color-primary)",
  color: "#fff",
};
const assistantBubble: React.CSSProperties = {
  ...bubble,
  alignSelf: "flex-start",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
};
const warningBox: React.CSSProperties = {
  display: "flex",
  gap: "0.4rem",
  alignItems: "flex-start",
  color: "var(--color-warning)",
  fontWeight: 600,
  fontSize: "0.85rem",
  marginBottom: "0.4rem",
};
const chip: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: "0.3rem",
  fontSize: "0.8rem",
  padding: "0.25rem 0.55rem",
  borderRadius: 999,
  border: "1px solid var(--color-border)",
  background: "var(--color-bg)",
  color: "var(--color-text)",
  textDecoration: "none",
};
const composer: React.CSSProperties = {
  position: "sticky",
  bottom: "calc(var(--nav-h) + var(--safe-bottom) + 0.5rem)",
  display: "flex",
  gap: "0.5rem",
  background: "var(--color-bg)",
  paddingTop: "0.5rem",
};
const composerInput: React.CSSProperties = {
  flex: 1,
  minWidth: 0,
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.7rem 0.85rem",
  color: "var(--color-text)",
  fontSize: "1rem",
  outline: "none",
};
