import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { aiSettings, resetAiSettings, saveAiSettings } from "../services/ai";
import { useToast } from "../contexts/ToastContext";
import { Button } from "./Button";
import type { AiProvider, AiSettings, AiSettingsInput } from "../types";

// AiSettingsCard shows the server's assistant model. Admins can set the
// endpoint, model and API key or fall back to the .env default.
export function AiSettingsCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const [settings, setSettings] = useState<AiSettings | null>(null);
  const [draft, setDraft] = useState<AiSettingsInput>({
    provider: "openai",
    base_url: "",
    model: "",
    vision_model: "",
  });
  const [apiKey, setApiKey] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [busy, setBusy] = useState(false);

  function apply(s: AiSettings) {
    setSettings(s);
    setDraft({
      provider: s.provider || "openai",
      base_url: s.base_url,
      model: s.model,
      vision_model: s.vision_model,
    });
    setApiKey("");
    setClearKey(false);
  }

  useEffect(() => {
    aiSettings().then((res) => {
      if (res.data) apply(res.data);
    });
  }, []);

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    const input: AiSettingsInput = { ...draft };
    if (clearKey) input.api_key = "";
    else if (apiKey.trim()) input.api_key = apiKey.trim();

    setBusy(true);
    const res = await saveAiSettings(input);
    setBusy(false);
    if (res.error || !res.data) {
      toast.error(res.error?.message ?? t("ai.settings_failed"));
      return;
    }
    apply(res.data);
    toast.success(t("ai.settings_saved"));
  }

  async function handleReset() {
    setBusy(true);
    const res = await resetAiSettings();
    setBusy(false);
    if (res.error || !res.data) {
      toast.error(res.error?.message ?? t("ai.settings_failed"));
      return;
    }
    apply(res.data);
    toast.success(t("ai.settings_reset_done"));
  }

  if (!settings) return null;

  const summary =
    settings.source === "none"
      ? t("ai.source_none")
      : t(settings.source === "web" ? "ai.source_web" : "ai.source_env", {
          model: settings.model,
        });

  return (
    <section style={card}>
      <h3 style={cardTitle}>{t("ai.settings_title")}</h3>
      <p style={cardDesc}>{summary}</p>

      {settings.can_edit && (
        <form
          onSubmit={handleSave}
          style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}
        >
          <label style={lbl}>
            <span style={lblText}>{t("ai.provider")}</span>
            <select
              style={inp}
              value={draft.provider}
              onChange={(e) => setDraft({ ...draft, provider: e.target.value as AiProvider })}
            >
              <option value="openai">{t("ai.provider_openai")}</option>
              <option value="anthropic">Anthropic</option>
            </select>
          </label>
          <label style={lbl}>
            <span style={lblText}>{t("ai.base_url")}</span>
            <input
              style={inp}
              type="url"
              value={draft.base_url}
              onChange={(e) => setDraft({ ...draft, base_url: e.target.value })}
              placeholder="http://localhost:11434/v1"
              maxLength={300}
            />
          </label>
          <label style={lbl}>
            <span style={lblText}>{t("ai.model")} *</span>
            <input
              style={inp}
              value={draft.model}
              onChange={(e) => setDraft({ ...draft, model: e.target.value })}
              placeholder="qwen2.5vl:7b"
              maxLength={100}
              required
            />
          </label>
          <label style={lbl}>
            <span style={lblText}>{t("ai.vision_model")}</span>
            <input
              style={inp}
              value={draft.vision_model}
              onChange={(e) => setDraft({ ...draft, vision_model: e.target.value })}
              placeholder={t("ai.vision_model_placeholder")}
              maxLength={100}
            />
          </label>
          <label style={lbl}>
            <span style={lblText}>{t("ai.api_key")}</span>
            <input
              style={inp}
              type="password"
              autoComplete="off"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={
                settings.source === "web" && settings.api_key_set
                  ? t("ai.api_key_stored")
                  : t("ai.api_key_placeholder")
              }
              disabled={clearKey}
              maxLength={500}
            />
          </label>
          {settings.source === "web" && settings.api_key_set && (
            <label
              style={{ display: "flex", gap: "0.5rem", alignItems: "center", fontSize: "0.85rem" }}
            >
              <input
                type="checkbox"
                checked={clearKey}
                onChange={(e) => setClearKey(e.target.checked)}
              />
              {t("ai.api_key_clear")}
            </label>
          )}
          <p style={{ ...cardDesc, margin: 0, fontSize: "0.8rem" }}>{t("ai.privacy_note")}</p>
          <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
            <Button type="submit" variant="primary" size="sm" disabled={busy}>
              {t("common.save")}
            </Button>
            {settings.source === "web" && (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={handleReset}
                disabled={busy}
              >
                {t("ai.settings_reset")}
              </Button>
            )}
          </div>
        </form>
      )}
    </section>
  );
}

const card: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius-lg)",
  padding: "1.25rem",
};
const cardTitle: React.CSSProperties = { fontSize: "1rem", fontWeight: 600 };
const cardDesc: React.CSSProperties = {
  color: "var(--color-text-muted)",
  fontSize: "0.9rem",
  margin: "0.5rem 0 1rem",
};
const lbl: React.CSSProperties = { display: "flex", flexDirection: "column", gap: "0.35rem" };
const lblText: React.CSSProperties = { fontSize: "0.85rem", color: "var(--color-text-muted)" };
const inp: React.CSSProperties = {
  background: "var(--color-bg)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.6rem 0.75rem",
  color: "var(--color-text)",
  fontSize: "0.95rem",
  outline: "none",
};
