import { useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { apiClient } from "../services/api";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import type { HouseholdDetail, Invite } from "../types";

export default function Household() {
  const { user } = useAuth();
  const toast = useToast();
  const { t } = useTranslation();
  const [detail, setDetail] = useState<HouseholdDetail | null>(null);
  const [invite, setInvite] = useState<Invite | null>(null);
  const [loading, setLoading] = useState(true);
  const [removeTarget, setRemoveTarget] = useState<{ id: string; name: string } | null>(null);

  useEffect(() => {
    apiClient.get<HouseholdDetail>("/api/households/current").then((res) => {
      if (res.data) setDetail(res.data);
      else if (res.error) toast.error(res.error.message);
      setLoading(false);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function handleInvite() {
    if (!detail) return;
    setInvite(null);
    const res = await apiClient.post<Invite>(`/api/households/${detail.id}/invite`, {});
    if (res.data) {
      setInvite(res.data);
      toast.success(t("household.invite_generated"));
    } else {
      toast.error(res.error?.message ?? t("household.invite_generated"));
    }
  }

  async function copyCode(code: string) {
    try {
      await navigator.clipboard.writeText(code);
      toast.success(t("household.copied"));
    } catch {
      toast.error(t("household.copy_failed"));
    }
  }

  async function confirmRemove() {
    if (!detail || !removeTarget) return;
    const { id: userId, name: username } = removeTarget;
    setRemoveTarget(null);

    const res = await apiClient.delete(`/api/households/${detail.id}/members/${userId}`);
    if (res.error) {
      toast.error(res.error.message);
      return;
    }
    toast.success(t("household.removed", { name: username }));
    setDetail((d) => (d ? { ...d, members: d.members.filter((m) => m.user_id !== userId) } : d));
  }

  const isOwner = detail?.members.find((m) => m.user_id === user?.id)?.role === "owner";

  if (loading) {
    return (
      <p style={{ color: "var(--color-text-muted)", textAlign: "center", marginTop: "2rem" }}>
        {t("common.loading")}
      </p>
    );
  }

  if (!detail) {
    return (
      <EmptyState
        icon="warning"
        title={t("household.load_error_title")}
        description={t("household.load_error_description")}
      />
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "1.25rem" }}>
      <section style={card}>
        <h2 style={{ fontSize: "1.1rem", marginBottom: "0.5rem" }}>{detail.name}</h2>
        <div style={{ color: "var(--color-text-muted)", fontSize: "0.9rem" }}>
          {t("household.member_count", { count: detail.members.length })}
        </div>
      </section>

      <section style={card}>
        <h3 style={cardTitle}>{t("household.members_title")}</h3>
        <ul style={{ listStyle: "none", marginTop: "0.5rem" }}>
          {detail.members.map((m) => (
            <li key={m.user_id} style={memberRow}>
              <div>
                <strong>{m.username}</strong>
                {m.user_id === user?.id && <span style={youBadge}>{t("household.you_badge")}</span>}
                <div
                  style={{
                    fontSize: "0.8rem",
                    color: "var(--color-text-muted)",
                    textTransform: "capitalize",
                  }}
                >
                  {m.role}
                </div>
              </div>
              {isOwner && m.user_id !== user?.id && (
                <Button
                  variant="danger"
                  size="sm"
                  onClick={() => setRemoveTarget({ id: m.user_id, name: m.username })}
                >
                  {t("household.remove")}
                </Button>
              )}
            </li>
          ))}
        </ul>
      </section>

      {isOwner && (
        <section style={card}>
          <h3 style={cardTitle}>{t("household.invite_title")}</h3>
          <p
            style={{
              color: "var(--color-text-muted)",
              fontSize: "0.9rem",
              margin: "0.5rem 0 1rem",
            }}
          >
            {t("household.invite_description")}
          </p>
          <Button
            variant="primary"
            onClick={handleInvite}
            iconLeft={<Icon name="plus" size={18} />}
          >
            {t("household.new_invite")}
          </Button>
          {invite && (
            <button
              onClick={() => copyCode(invite.code)}
              style={codeBlock}
              title={t("household.tap_to_copy")}
            >
              <span style={{ fontSize: "1.6rem", letterSpacing: "0.18em", fontWeight: 700 }}>
                {invite.code}
              </span>
              <span
                style={{
                  fontSize: "0.8rem",
                  color: "var(--color-text-muted)",
                  marginTop: "0.25rem",
                }}
              >
                {t("household.tap_to_copy")} ·{" "}
                {t("household.expires", { date: new Date(invite.expires_at).toLocaleDateString() })}
              </span>
            </button>
          )}
        </section>
      )}

      <section style={card}>
        <h3 style={cardTitle}>{t("household.join_title")}</h3>
        <p
          style={{ color: "var(--color-text-muted)", fontSize: "0.9rem", margin: "0.5rem 0 1rem" }}
        >
          {t("household.join_description")}
        </p>
        <Link to="/household/join" style={{ textDecoration: "none" }}>
          <Button variant="secondary">{t("household.enter_code")}</Button>
        </Link>
      </section>

      <ConfirmDialog
        open={removeTarget !== null}
        title={removeTarget ? t("household.remove_confirm_title", { name: removeTarget.name }) : ""}
        description={t("household.remove_confirm_description")}
        confirmLabel={t("household.remove")}
        destructive
        onConfirm={confirmRemove}
        onCancel={() => setRemoveTarget(null)}
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
const memberRow: React.CSSProperties = {
  display: "flex",
  justifyContent: "space-between",
  alignItems: "center",
  padding: "0.6rem 0",
  borderBottom: "1px solid var(--color-border)",
};
const youBadge: React.CSSProperties = {
  marginLeft: "0.5rem",
  fontSize: "0.7rem",
  background: "var(--color-accent)",
  color: "#fff",
  padding: "0.1rem 0.4rem",
  borderRadius: 4,
  textTransform: "uppercase",
  letterSpacing: "0.05em",
};
const codeBlock: React.CSSProperties = {
  marginTop: "1rem",
  width: "100%",
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  background: "var(--color-bg)",
  border: "1px dashed var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "1rem",
};
