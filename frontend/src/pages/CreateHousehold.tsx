import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiClient } from "../services/api";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { rebuildLocalCacheFromServer } from "../services/sync";
import { Button } from "../components/Button";
import type { Household } from "../types";

// CreateHousehold lets a user spin up a new household and become its owner.
// The backend automatically switches their active household to the new one,
// so we wipe the local cache + re-sync just like the join flow.
export default function CreateHousehold() {
  const navigate = useNavigate();
  const toast = useToast();
  const { t } = useTranslation();
  const { refreshHouseholds } = useAuth();

  const [name, setName] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!name.trim()) return;

    setLoading(true);
    const res = await apiClient.post<Household>("/api/households", { name: name.trim() });
    if (res.error) {
      setLoading(false);
      toast.error(res.error.message);
      return;
    }

    await rebuildLocalCacheFromServer();
    await refreshHouseholds();
    setLoading(false);
    toast.success(t("create_household.success", { name: name.trim() }));
    navigate("/medicines");
  }

  return (
    <div style={{ maxWidth: 420 }}>
      <p style={{ color: "var(--color-text-muted)", marginBottom: "1.5rem", fontSize: "0.95rem" }}>
        {t("create_household.description")}
      </p>

      <form
        onSubmit={handleSubmit}
        style={{ display: "flex", flexDirection: "column", gap: "1rem" }}
      >
        <input
          style={inp}
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("create_household.placeholder")}
          maxLength={80}
          autoFocus
          required
        />
        <Button type="submit" disabled={loading || !name.trim()} fullWidth>
          {loading ? t("create_household.creating") : t("create_household.create_button")}
        </Button>
      </form>
    </div>
  );
}

const inp: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.85rem 1rem",
  color: "var(--color-text)",
  fontSize: "1rem",
};
