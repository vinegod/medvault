import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Icon } from "../components/Icon";

// Shown right after register: pick whether to keep the auto-created
// personal household or join an existing one via invite code. "Skip"
// keeps the personal household and goes to /medicines.
export default function OnboardingHousehold() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  return (
    <div style={page}>
      <div style={card}>
        <h1 style={title}>{t("onboarding_household.title")}</h1>
        <p style={subtitle}>{t("onboarding_household.subtitle")}</p>

        <div style={choices}>
          <button onClick={() => navigate("/medicines")} style={choiceBtn}>
            <Icon name="home" size={28} />
            <div style={choiceTitle}>{t("onboarding_household.fresh_title")}</div>
            <div style={choiceDesc}>{t("onboarding_household.fresh_desc")}</div>
          </button>

          <Link to="/household/join" style={{ textDecoration: "none" }}>
            <div style={choiceBtn}>
              <Icon name="users" size={28} />
              <div style={choiceTitle}>{t("onboarding_household.invite_title")}</div>
              <div style={choiceDesc}>{t("onboarding_household.invite_desc")}</div>
            </div>
          </Link>
        </div>

        <button onClick={() => navigate("/medicines")} style={skipBtn}>
          {t("onboarding_household.skip")}
        </button>
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
  maxWidth: 480,
};
const title: React.CSSProperties = { fontSize: "1.6rem", marginBottom: "0.4rem" };
const subtitle: React.CSSProperties = {
  color: "var(--color-text-muted)",
  marginBottom: "1.5rem",
};
const choices: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "1fr 1fr",
  gap: "0.75rem",
  marginBottom: "1rem",
};
const choiceBtn: React.CSSProperties = {
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  gap: "0.5rem",
  padding: "1.25rem 0.75rem",
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius-lg)",
  color: "var(--color-text)",
  cursor: "pointer",
  textAlign: "center",
  width: "100%",
};
const choiceTitle: React.CSSProperties = { fontWeight: 600, fontSize: "0.95rem" };
const choiceDesc: React.CSSProperties = {
  fontSize: "0.8rem",
  color: "var(--color-text-muted)",
};
const skipBtn: React.CSSProperties = {
  width: "100%",
  background: "transparent",
  border: "none",
  color: "var(--color-text-muted)",
  padding: "0.75rem",
  fontSize: "0.9rem",
  cursor: "pointer",
};
