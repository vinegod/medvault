import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiClient } from "../services/api";
import { db, deleteMeta } from "../services/db";
import { performSync } from "../services/sync";
import { useToast } from "../contexts/ToastContext";
import { Button } from "../components/Button";

export default function JoinHousehold() {
  const navigate = useNavigate();
  const toast = useToast();
  const { t } = useTranslation();
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);

    const res = await apiClient.post<{ household_id: string }>("/api/households/join", {
      code: code.trim().toUpperCase(),
    });

    if (res.data) {
      await db.medicines.clear();
      await deleteMeta("last_sync");
      await performSync();

      setLoading(false);
      toast.success(t("join_household.success"));
      navigate("/household");
    } else {
      setLoading(false);
      toast.error(res.error?.message ?? t("join_household.invalid_code"));
    }
  }

  return (
    <div style={{ maxWidth: 400 }}>
      <p style={{ color: "var(--color-text-muted)", marginBottom: "1.5rem", fontSize: "0.95rem" }}>
        {t("join_household.description")}
      </p>

      <form
        onSubmit={handleSubmit}
        style={{ display: "flex", flexDirection: "column", gap: "1rem" }}
      >
        <input
          style={inp}
          value={code}
          onChange={(e) => setCode(e.target.value.toUpperCase())}
          placeholder={t("join_household.placeholder")}
          maxLength={8}
          autoCapitalize="characters"
          autoFocus
          required
        />

        <Button type="submit" disabled={loading || code.trim().length < 6} fullWidth>
          {loading ? t("join_household.joining") : t("join_household.join_button")}
        </Button>
      </form>
    </div>
  );
}

const inp: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "1rem",
  color: "var(--color-text)",
  fontSize: "1.6rem",
  textAlign: "center",
  letterSpacing: "0.18em",
  fontFamily: "ui-monospace, monospace",
};
