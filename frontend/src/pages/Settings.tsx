import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { useTranslation, Trans } from "react-i18next";
import i18n from "../i18n";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { exportAll, importData, getMeta, setMeta } from "../services/db";
import { useInstallPrompt } from "../hooks/useInstallPrompt";
import { useTheme, type Theme } from "../hooks/useTheme";
import { apiClient } from "../services/api";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { encryptJSON, decryptJSON, type EncryptedExport } from "../utils/crypto";
import {
  isEnabled as notificationsEnabled,
  notificationPermission,
  requestPermissionAndRegister,
  disable as disableNotifications,
} from "../services/notifications";
import { DEFAULT_LOW_STOCK } from "../constants";

export default function Settings() {
  const { status, user, serverUrl, disconnect } = useAuth();
  const toast = useToast();
  const { t } = useTranslation();
  const { canInstall, install } = useInstallPrompt();
  const { theme, setTheme } = useTheme();
  const [confirmDisconnect, setConfirmDisconnect] = useState(false);
  const [pwCurrent, setPwCurrent] = useState("");
  const [pwNew, setPwNew] = useState("");
  const [pwBusy, setPwBusy] = useState(false);
  const [threshold, setThreshold] = useState(DEFAULT_LOW_STOCK);
  const [encryptExport, setEncryptExport] = useState(false);
  const [exportPassphrase, setExportPassphrase] = useState("");
  const [notifEnabled, setNotifEnabled] = useState(notificationsEnabled());
  const isConnected = status === "connected";

  useEffect(() => {
    getMeta("low_stock_threshold").then((v) => {
      const n = v ? Number.parseInt(v, 10) : DEFAULT_LOW_STOCK;
      if (!Number.isNaN(n)) setThreshold(n);
    });
  }, []);

  async function handleChangePassword(e: React.FormEvent) {
    e.preventDefault();
    setPwBusy(true);
    try {
      const res = await apiClient.post<{ access_token: string; refresh_token: string }>(
        "/api/auth/password",
        { current_password: pwCurrent, new_password: pwNew }
      );
      if (res.error) {
        toast.error(res.error.message ?? t("settings.password_failed"));
        return;
      }
      if (res.data) {
        apiClient.setAccessToken(res.data.access_token);
        await setMeta("access_token", res.data.access_token);
        await setMeta("refresh_token", res.data.refresh_token);
      }
      toast.success(t("settings.password_changed"));
      setPwCurrent("");
      setPwNew("");
    } finally {
      setPwBusy(false);
    }
  }

  async function handleThresholdChange(value: number) {
    const clamped = Math.max(1, Math.min(99, value));
    setThreshold(clamped);
    await setMeta("low_stock_threshold", String(clamped));
  }

  async function handleExport() {
    const json = await exportAll();
    let payload = json;

    // Optional passphrase encryption. Wraps the v1 plaintext in the v2
    // envelope so importers can detect format-by-shape.
    if (encryptExport) {
      if (exportPassphrase.length < 8) {
        toast.error(t("settings.passphrase_too_short"));
        return;
      }
      const env = await encryptJSON(json, exportPassphrase);
      payload = JSON.stringify(env, null, 2);
    }

    const blob = new Blob([payload], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    const suffix = encryptExport ? ".enc.json" : ".json";
    a.download = `medvault-export-${new Date().toISOString().split("T")[0]}${suffix}`;
    a.click();
    URL.revokeObjectURL(url);
    setExportPassphrase("");
    toast.success(t("settings.exported"));
  }

  async function handleImport(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    let text = await file.text();

    // Detect the encrypted v2 shape by parsing the outer envelope. v1 has
    // {version:1, medicines:[...]}; v2 has {version:2, encrypted:true, ...}.
    try {
      const sniff = JSON.parse(text) as Partial<EncryptedExport>;
      if (sniff.version === 2 && sniff.encrypted) {
        const passphrase = window.prompt(t("settings.import_passphrase_prompt"));
        if (!passphrase) {
          e.target.value = "";
          return;
        }
        try {
          text = await decryptJSON(sniff as EncryptedExport, passphrase);
        } catch {
          toast.error(t("settings.import_decrypt_failed"));
          e.target.value = "";
          return;
        }
      }
    } catch {
      // Outer parse failed — fall through to importData, which surfaces a
      // more specific error message.
    }

    try {
      const count = await importData(text);
      toast.success(t("settings.imported", { count }));
    } catch {
      toast.error(t("settings.import_failed"));
    }
    e.target.value = "";
  }

  async function handleNotificationsToggle() {
    if (notifEnabled) {
      await disableNotifications();
      setNotifEnabled(false);
      toast.info(t("settings.notifications_disabled"));
      return;
    }
    const ok = await requestPermissionAndRegister();
    setNotifEnabled(ok);
    if (ok) {
      toast.success(t("settings.notifications_enabled"));
    } else if (notificationPermission() === "denied") {
      toast.error(t("settings.notifications_blocked"));
    }
  }

  async function handleDisconnect() {
    setConfirmDisconnect(false);
    await disconnect();
    toast.info(t("settings.disconnected_toast"));
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "1.75rem" }}>
      <div style={group}>
        <h2 style={groupTitle}>{t("settings.group_account")}</h2>
        <div className="grid-2col-md">
          <section style={card}>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: "0.75rem",
                marginBottom: "0.75rem",
              }}
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

          {isConnected && (
            <section style={card}>
              <h3 style={{ ...cardTitle, marginBottom: "0.75rem" }}>
                {t("settings.password_title")}
              </h3>
              <form
                onSubmit={handleChangePassword}
                style={{ display: "flex", flexDirection: "column", gap: "0.6rem" }}
              >
                <input
                  type="password"
                  placeholder={t("settings.password_current")}
                  value={pwCurrent}
                  onChange={(e) => setPwCurrent(e.target.value)}
                  autoComplete="current-password"
                  style={inputStyle}
                  required
                />
                <input
                  type="password"
                  placeholder={t("settings.password_new")}
                  value={pwNew}
                  onChange={(e) => setPwNew(e.target.value)}
                  autoComplete="new-password"
                  style={inputStyle}
                  required
                  minLength={8}
                />
                <Button
                  type="submit"
                  variant="secondary"
                  disabled={pwBusy || !pwCurrent || pwNew.length < 8}
                >
                  {pwBusy ? t("settings.password_saving") : t("settings.password_save")}
                </Button>
              </form>
            </section>
          )}
        </div>
      </div>

      <div style={group}>
        <h2 style={groupTitle}>{t("settings.group_data")}</h2>
        <div className="grid-2col-md">
          <section style={card}>
            <h3 style={cardTitle}>{t("settings.backup_title")}</h3>
            <p
              style={{ color: "var(--color-text-muted)", fontSize: "0.9rem", marginBottom: "1rem" }}
            >
              {t("settings.backup_description")}
            </p>
            <label
              style={{
                display: "flex",
                alignItems: "center",
                gap: "0.5rem",
                marginBottom: "0.5rem",
                fontSize: "0.9rem",
              }}
            >
              <input
                type="checkbox"
                checked={encryptExport}
                onChange={(e) => setEncryptExport(e.target.checked)}
              />
              <span>{t("settings.backup_encrypt")}</span>
            </label>
            {encryptExport && (
              <input
                type="password"
                autoComplete="new-password"
                placeholder={t("settings.passphrase_placeholder")}
                value={exportPassphrase}
                onChange={(e) => setExportPassphrase(e.target.value)}
                style={{ ...inputStyle, marginBottom: "0.75rem" }}
                minLength={8}
              />
            )}
            <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
              <Button
                variant="secondary"
                onClick={handleExport}
                iconLeft={<Icon name="download" size={18} />}
                disabled={encryptExport && exportPassphrase.length < 8}
              >
                {t("common.export")}
              </Button>
              <label style={importBtn}>
                <Icon name="upload" size={18} />
                <span>{t("common.import")}</span>
                <input
                  type="file"
                  accept=".json"
                  onChange={handleImport}
                  style={{ display: "none" }}
                />
              </label>
            </div>
          </section>

          <section style={card}>
            <h3 style={{ ...cardTitle, marginBottom: "0.25rem" }}>
              {t("settings.threshold_title")}
            </h3>
            <p
              style={{
                color: "var(--color-text-muted)",
                fontSize: "0.85rem",
                marginBottom: "0.75rem",
              }}
            >
              {t("settings.threshold_description")}
            </p>
            <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
              <button
                onClick={() => handleThresholdChange(threshold - 1)}
                disabled={threshold <= 1}
                style={{ ...threshBtn, opacity: threshold <= 1 ? 0.35 : 1 }}
                aria-label="-"
              >
                <Icon name="minus" size={16} />
              </button>
              <span
                style={{ fontWeight: 700, fontSize: "1.1rem", minWidth: 28, textAlign: "center" }}
              >
                {threshold}
              </span>
              <button
                onClick={() => handleThresholdChange(threshold + 1)}
                disabled={threshold >= 99}
                style={{ ...threshBtn, opacity: threshold >= 99 ? 0.35 : 1 }}
                aria-label="+"
              >
                <Icon name="plus" size={16} />
              </button>
            </div>
          </section>
        </div>
      </div>

      <div style={group}>
        <h2 style={groupTitle}>{t("settings.group_preferences")}</h2>
        <div className="grid-2col-md">
          <section style={card}>
            <h3 style={{ ...cardTitle, marginBottom: "0.25rem" }}>
              {t("settings.notifications_title")}
            </h3>
            <p
              style={{
                color: "var(--color-text-muted)",
                fontSize: "0.85rem",
                marginBottom: "0.75rem",
              }}
            >
              {t("settings.notifications_description")}
            </p>
            <Button
              variant={notifEnabled ? "danger" : "primary"}
              onClick={handleNotificationsToggle}
              iconLeft={<Icon name="bell" size={16} />}
            >
              {notifEnabled
                ? t("settings.notifications_disable")
                : t("settings.notifications_enable")}
            </Button>
            {notificationPermission() === "denied" && !notifEnabled && (
              <p
                style={{
                  color: "var(--color-error)",
                  fontSize: "0.8rem",
                  marginTop: "0.5rem",
                }}
              >
                {t("settings.notifications_browser_blocked")}
              </p>
            )}
          </section>

          {canInstall && (
            <section style={card}>
              <h3 style={cardTitle}>{t("settings.pwa_title")}</h3>
              <p
                style={{
                  color: "var(--color-text-muted)",
                  fontSize: "0.9rem",
                  marginBottom: "1rem",
                }}
              >
                {t("settings.pwa_description")}
              </p>
              <Button variant="primary" onClick={install}>
                {t("common.install")}
              </Button>
            </section>
          )}

          <section style={card}>
            <h3 style={{ ...cardTitle, marginBottom: "0.75rem" }}>
              {t("settings.appearance_title")}
            </h3>
            <div style={{ display: "flex", gap: "0.5rem" }}>
              {(["system", "light", "dark"] as Theme[]).map((mode) => (
                <button
                  key={mode}
                  onClick={() => setTheme(mode)}
                  style={{
                    ...langBtn,
                    background: theme === mode ? "var(--color-primary)" : "var(--color-surface-2)",
                    color: theme === mode ? "#fff" : "var(--color-text)",
                    borderColor: theme === mode ? "var(--color-primary)" : "var(--color-border)",
                  }}
                >
                  {t(`settings.theme_${mode}`)}
                </button>
              ))}
            </div>
          </section>

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
                      i18n.language === lang ? "var(--color-primary)" : "var(--color-surface-2)",
                    color: i18n.language === lang ? "#fff" : "var(--color-text)",
                    borderColor:
                      i18n.language === lang ? "var(--color-primary)" : "var(--color-border)",
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
        </div>
      </div>

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

const group: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "1rem",
};

const groupTitle: React.CSSProperties = {
  fontSize: "0.8rem",
  fontWeight: 700,
  textTransform: "uppercase",
  letterSpacing: "0.08em",
  color: "var(--color-text-muted)",
  margin: "0 0.25rem 0.25rem",
};

const cardTitle: React.CSSProperties = { fontSize: "1rem", fontWeight: 600 };

const langBtn: React.CSSProperties = {
  padding: "0.5rem 1.1rem",
  borderRadius: "var(--radius)",
  border: "1px solid",
  fontSize: "0.95rem",
  fontWeight: 600,
  cursor: "pointer",
  minHeight: "var(--tap)",
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

const inputStyle: React.CSSProperties = {
  width: "100%",
  background: "var(--color-surface-2)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.625rem 0.75rem",
  color: "var(--color-text)",
};

const threshBtn: React.CSSProperties = {
  width: "var(--tap)",
  height: "var(--tap)",
  borderRadius: "50%",
  background: "var(--color-surface-2)",
  border: "1px solid var(--color-border)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
};
