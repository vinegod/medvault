import { useState, type FormEvent } from "react";
import { useNavigate, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { Button } from "../components/Button";

export default function Register() {
  const { connect } = useAuth();
  const navigate = useNavigate();
  const toast = useToast();
  const { t } = useTranslation();

  const [serverUrl, setServerUrl] = useState("http://localhost:8080");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    const result = await connect(serverUrl.trim().replace(/\/$/, ""), username, password, true);
    setLoading(false);
    if (result.success) {
      toast.success(t("auth.account_created"));
      navigate("/medicines");
    } else {
      toast.error(result.error ?? t("auth.registration_failed"));
    }
  }

  return (
    <div style={page}>
      <div style={card}>
        <h1 style={title}>{t("auth.title")}</h1>
        <p style={subtitle}>{t("auth.register_subtitle")}</p>

        <form onSubmit={handleSubmit} style={form}>
          <label style={lbl}>
            {t("auth.server_url")}
            <input
              style={inp}
              type="url"
              value={serverUrl}
              onChange={(e) => setServerUrl(e.target.value)}
              required
            />
          </label>

          <label style={lbl}>
            {t("auth.username")}
            <input
              style={inp}
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              required
              autoFocus
            />
            <small style={hint}>{t("auth.username_hint")}</small>
          </label>

          <label style={lbl}>
            {t("auth.password")}
            <input
              style={inp}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
            />
            <small style={hint}>{t("auth.password_hint")}</small>
          </label>

          <Button type="submit" disabled={loading} fullWidth size="lg">
            {loading ? t("auth.creating_account") : t("common.register")}
          </Button>
        </form>

        <div style={footer}>
          <p>
            {t("auth.have_account")} <Link to="/login">{t("common.log_in")}</Link>
          </p>
          <p style={{ marginTop: "0.5rem" }}>
            <Link to="/medicines">{t("auth.continue_without")}</Link>
          </p>
        </div>
      </div>
    </div>
  );
}

const page: React.CSSProperties = {
  minHeight: "100dvh",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  padding: "1rem",
};
const card: React.CSSProperties = {
  width: "100%",
  maxWidth: 420,
  background: "var(--color-surface)",
  borderRadius: "var(--radius-lg)",
  padding: "2rem",
  border: "1px solid var(--color-border)",
};
const title: React.CSSProperties = { fontSize: "1.8rem", marginBottom: "0.25rem" };
const subtitle: React.CSSProperties = { color: "var(--color-text-muted)", marginBottom: "1.5rem" };
const form: React.CSSProperties = { display: "flex", flexDirection: "column", gap: "1rem" };
const lbl: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "0.4rem",
  fontSize: "0.85rem",
  color: "var(--color-text-muted)",
};
const inp: React.CSSProperties = {
  background: "var(--color-bg)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.7rem 0.85rem",
  color: "var(--color-text)",
  fontSize: "1rem",
};
const hint: React.CSSProperties = {
  fontSize: "0.75rem",
  color: "var(--color-text-muted)",
  marginTop: 2,
};
const footer: React.CSSProperties = {
  marginTop: "1.5rem",
  fontSize: "0.9rem",
  color: "var(--color-text-muted)",
  textAlign: "center",
};
