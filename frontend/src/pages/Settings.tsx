import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation, Trans } from "react-i18next";
import i18n from "../i18n";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { exportAll, importData } from "../services/db";
import { useInstallPrompt } from "../hooks/useInstallPrompt";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { ConfirmDialog } from "../components/ConfirmDialog";

export default function Settings() {
  const { status, user, serverUrl, disconnect } = useAuth();
  const toast = useToast();
  const { t } = useTranslation();
  const { canInstall, install } = useInstallPrompt();
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const isConnected = status === "connected";

  async function handleExport() {
    const json = await exportAll();
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `medvault-export-${new Date().toISOString().split("T")[0]}.json`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success(t("settings.exported"));
  }

  async function handleImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const text = await file.text();
    try {
      const count = await importData(text);
      toast.success(t("settings.imported", { count }));
    } catch {
      toast.error(t("settings.import_failed"));
    }
    e.target.value = "";
  }

  async function handleDisconnect() {
    setConfirmDisconnect(false);
    await disconnect();
    toast.info(t("settings.disconnected_toast"));
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "1.25rem" }}>
      <section style={card}>
        <div
          style={{ display: "flex", alignItems: "center", gap: "0.75rem", marginBottom: "0.75rem" }}
        >
          <Icon
            name={isConnected ? "wifi" : "wifi-off"}
            size={20}
            style={{ color: isConnected ? "var(--color-success)" : "var(--color-text-muted)" }}
          />
          <h3 style={cardTitle}>{t("settings.server_title")}</h3>
        </div>

        {isConnected ? (
          <>
            <div style={{ marginBottom: "1rem" }}>
              <div>
                {t("settings.connected_as")} <strong>{user?.username}</strong>
              </div>
              <div
                style={{
                  color: "var(--color-text-muted)",
                  fontSize: "0.85rem",
                  marginTop: "0.25rem",
                }}
              >
                {serverUrl}
              </div>
            </div>
            <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
              <Link to="/household" style={{ textDecoration: "none" }}>
                <Button variant="secondary" iconLeft={<Icon name="users" size={18} />}>
                  {t("header.household")}
                </Button>
              </Link>
              <Button
                variant="danger"
                onClick={() => setConfirmDisconnect(true)}
                iconLeft={<Icon name="logout" size={18} />}
              >
                {t("common.disconnect")}
              </Button>
            </div>
          </>
        ) : (
          <>
            <p style={{ color: "var(--color-text-muted)", marginBottom: "1rem" }}>
              {status === "error"
                ? t("settings.error_reconnect")
                : t("settings.disconnected_description")}
            </p>
            <div style={{ display: "flex", gap: "0.5rem" }}>
              <Link to="/login" style={{ flex: 1, textDecoration: "none" }}>
                <Button variant="primary" fullWidth>
                  {t("common.log_in")}
                </Button>
              </Link>
              <Link to="/register" style={{ flex: 1, textDecoration: "none" }}>
                <Button variant="secondary" fullWidth>
                  {t("common.register")}
                </Button>
              </Link>
            </div>
          </>
        )}
      </section>

      <section style={card}>
        <h3 style={cardTitle}>{t("settings.backup_title")}</h3>
        <p style={{ color: "var(--color-text-muted)", fontSize: "0.9rem", marginBottom: "1rem" }}>
          {t("settings.backup_description")}
        </p>
        <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
          <Button
            variant="secondary"
            onClick={handleExport}
            iconLeft={<Icon name="download" size={18} />}
          >
            {t("common.export")}
          </Button>
          <label style={importBtn}>
            <Icon name="upload" size={18} />
            <span>{t("common.import")}</span>
            <input type="file" accept=".json" onChange={handleImport} style={{ display: "none" }} />
          </label>
        </div>
      </section>

      {canInstall && (
        <section style={card}>
          <h3 style={cardTitle}>{t("settings.pwa_title")}</h3>
          <p style={{ color: "var(--color-text-muted)", fontSize: "0.9rem", marginBottom: "1rem" }}>
            {t("settings.pwa_description")}
          </p>
          <Button variant="primary" onClick={install}>
            {t("common.install")}
          </Button>
        </section>
      )}

      <section style={card}>
        <h3 style={cardTitle}>{t("language.title")}</h3>
        <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.75rem" }}>
          {(["en", "uk"] as const).map((lang) => (
            <button
              key={lang}
              onClick={() => i18n.changeLanguage(lang)}
              style={{
                ...langBtn,
                background:
                  i18n.language === lang ? "var(--color-accent)" : "var(--color-surface-2)",
                color: i18n.language === lang ? "#fff" : "var(--color-text)",
                borderColor: i18n.language === lang ? "var(--color-accent)" : "var(--color-border)",
              }}
            >
              {t(`language.${lang}`)}
            </button>
          ))}
        </div>
      </section>

      <section style={card}>
        <h3 style={cardTitle}>{t("settings.about_title")}</h3>
        <p style={{ color: "var(--color-text-muted)", fontSize: "0.9rem" }}>
          {t("settings.about_description")}
        </p>
      </section>

      <ConfirmDialog
        open={confirmDisconnect}
        title={t("settings.disconnect_dialog_title")}
        description={
          <Trans
            i18nKey="settings.disconnect_dialog_description"
            components={{ strong: <strong /> }}
          />
        }
        confirmLabel={t("settings.disconnect_confirm")}
        destructive
        onConfirm={handleDisconnect}
        onCancel={() => setConfirmDisconnect(false)}
      />
    </div>
  );
}

const card: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius-lg)",
  padding: "1.25rem",
};

const cardTitle: React.CSSProperties = { fontSize: "1rem", fontWeight: 600 };

const langBtn: React.CSSProperties = {
  padding: "0.5rem 1.1rem",
  borderRadius: "var(--radius)",
  border: "1px solid",
  fontSize: "0.95rem",
  fontWeight: 600,
  cursor: "pointer",
};

const importBtn: React.CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  gap: "0.5rem",
  background: "var(--color-surface-2)",
  color: "var(--color-text)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.625rem 1.1rem",
  fontSize: "0.95rem",
  fontWeight: 600,
  minHeight: 44,
  cursor: "pointer",
};
