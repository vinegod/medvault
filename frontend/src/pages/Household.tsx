import { useState, useEffect, useRef, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import QRCode from "qrcode";
import { apiClient } from "../services/api";
import { useAuth } from "../contexts/AuthContext";
import { useToast } from "../contexts/ToastContext";
import { rebuildLocalCacheFromServer } from "../services/sync";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { EmptyState } from "../components/EmptyState";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { Skeleton } from "../components/Skeleton";
import type { HouseholdDetail, Invite, AuditEntry } from "../types";
import { AuditList } from "../components/AuditList";

export default function Household() {
  const { user, refreshHouseholds } = useAuth();
  const toast = useToast();
  const { t } = useTranslation();
  const navigate = useNavigate();

  const [detail, setDetail] = useState<HouseholdDetail | null>(null);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [auditOpen, setAuditOpen] = useState(false);
  const [loading, setLoading] = useState(true);

  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState("");

  const [activeInvite, setActiveInvite] = useState<Invite | null>(null);
  const [showQr, setShowQr] = useState(false);

  const [removeTarget, setRemoveTarget] = useState<{ id: string; name: string } | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<string | null>(null);
  const [transferTarget, setTransferTarget] = useState<{ id: string; name: string } | null>(null);
  const [confirmLeave, setConfirmLeave] = useState(false);

  // Single load path used by both the initial render and post-action
  // refresh. Sequential because the invites endpoint needs the household
  // id; the previous "fetch in parallel with detail?.id ?? ''" issued a
  // malformed request whenever detail wasn't loaded yet.
  const reload = useCallback(async () => {
    setLoading(true);
    const cur = await apiClient.get<HouseholdDetail>("/api/households/current");
    if (cur.data) {
      setDetail(cur.data);
      const inv = await apiClient.get<Invite[]>(`/api/households/${cur.data.id}/invites`);
      if (inv.data) setInvites(inv.data);
      // Audit list is fetched alongside but the section stays collapsed by
      // default — most users won't open it.
      const aud = await apiClient.get<AuditEntry[]>(
        `/api/households/${cur.data.id}/audit?limit=50`
      );
      if (aud.data) setAudit(aud.data);
    } else if (cur.error) {
      toast.error(cur.error.message);
    }
    setLoading(false);
  }, [toast]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const isOwner = detail?.members.find((m) => m.user_id === user?.id)?.role === "owner";
  const otherMembers = detail?.members.filter((m) => m.user_id !== user?.id) ?? [];

  function startRename() {
    if (!detail) return;
    setNameDraft(detail.name);
    setEditingName(true);
  }

  async function saveName() {
    if (!detail) return;
    const trimmed = nameDraft.trim();
    if (!trimmed || trimmed === detail.name) {
      setEditingName(false);
      return;
    }
    const res = await apiClient.patch<{ name: string }>(`/api/households/${detail.id}`, {
      name: trimmed,
    });
    if (res.error) {
      toast.error(res.error.message);
      return;
    }
    setDetail((d) => (d ? { ...d, name: trimmed } : d));
    setEditingName(false);
    toast.success(t("household.rename_saved"));
    refreshHouseholds();
  }

  async function handleNewInvite() {
    if (!detail) return;
    setShowQr(false);
    const res = await apiClient.post<Invite>(`/api/households/${detail.id}/invite`, {});
    if (res.data) {
      setActiveInvite(res.data);
      setInvites((list) => [res.data!, ...list]);
      toast.success(t("household.invite_generated"));
    } else {
      toast.error(res.error?.message ?? "");
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

  async function confirmRevoke() {
    if (!detail || !revokeTarget) return;
    const code = revokeTarget;
    setRevokeTarget(null);
    const res = await apiClient.delete(`/api/households/${detail.id}/invites/${code}`);
    if (res.error) {
      toast.error(res.error.message);
      return;
    }
    setInvites((list) => list.filter((i) => i.code !== code));
    if (activeInvite?.code === code) {
      setActiveInvite(null);
      setShowQr(false);
    }
    toast.success(t("household.invite_revoked"));
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

  async function confirmTransfer() {
    if (!detail || !transferTarget) return;
    const target = transferTarget;
    setTransferTarget(null);
    const res = await apiClient.post(`/api/households/${detail.id}/transfer`, {
      to_user_id: target.id,
    });
    if (res.error) {
      toast.error(res.error.message);
      return;
    }
    toast.success(t("household.transfer_success", { name: target.name }));
    await reload();
    refreshHouseholds();
  }

  async function handleLeave() {
    if (!detail) return;
    setConfirmLeave(false);
    const res = await apiClient.delete<{ code?: string }>(
      `/api/households/${detail.id}/membership`
    );
    if (res.error) {
      if (res.error.code === "OWNER_TRANSFER_REQUIRED") {
        toast.error(t("household.leave_transfer_required"));
        return;
      }
      toast.error(res.error.message);
      return;
    }
    toast.success(t("household.left"));
    await rebuildLocalCacheFromServer();
    await refreshHouseholds();
    navigate("/medicines");
  }

  if (loading) {
    return (
      <div style={{ padding: "1rem" }}>
        <Skeleton count={3} height={72} />
      </div>
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
        {editingName ? (
          <div style={{ display: "flex", gap: "0.5rem", alignItems: "center" }}>
            <input
              autoFocus
              value={nameDraft}
              onChange={(e) => setNameDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") saveName();
                if (e.key === "Escape") setEditingName(false);
              }}
              maxLength={80}
              style={nameInput}
            />
            <Button variant="primary" size="sm" onClick={saveName}>
              {t("common.save")}
            </Button>
          </div>
        ) : (
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <h2 style={{ fontSize: "1.1rem" }}>{detail.name}</h2>
            {isOwner && (
              <button
                onClick={startRename}
                style={iconBtn}
                aria-label={t("household.rename_button")}
              >
                <Icon name="edit" size={16} />
              </button>
            )}
          </div>
        )}
        <div style={{ color: "var(--color-text-muted)", fontSize: "0.9rem", marginTop: "0.4rem" }}>
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

      {isOwner && otherMembers.length > 0 && (
        <section style={card}>
          <h3 style={cardTitle}>{t("household.transfer_title")}</h3>
          <p style={cardDesc}>{t("household.transfer_description")}</p>
          <div style={{ display: "flex", flexDirection: "column", gap: "0.4rem" }}>
            {otherMembers.map((m) => (
              <button
                key={m.user_id}
                style={transferRow}
                onClick={() => setTransferTarget({ id: m.user_id, name: m.username })}
              >
                <span>{m.username}</span>
                <span style={{ fontSize: "0.8rem", color: "var(--color-text-muted)" }}>
                  {t("household.transfer_to")}
                </span>
              </button>
            ))}
          </div>
        </section>
      )}

      {isOwner && (
        <section style={card}>
          <h3 style={cardTitle}>{t("household.invite_title")}</h3>
          <p style={cardDesc}>{t("household.invite_description")}</p>
          <Button
            variant="primary"
            onClick={handleNewInvite}
            iconLeft={<Icon name="plus" size={18} />}
          >
            {t("household.new_invite")}
          </Button>
          {activeInvite && (
            <ActiveInviteBlock
              invite={activeInvite}
              showQr={showQr}
              onToggleQr={() => setShowQr((v) => !v)}
              onCopy={copyCode}
            />
          )}

          {invites.length > 0 && (
            <>
              <h4
                style={{
                  marginTop: "1.25rem",
                  fontSize: "0.85rem",
                  color: "var(--color-text-muted)",
                }}
              >
                {t("household.active_invites")}
              </h4>
              <ul style={{ listStyle: "none", marginTop: "0.5rem" }}>
                {invites.map((inv) => (
                  <li key={inv.code} style={inviteRow}>
                    <button
                      onClick={() => copyCode(inv.code)}
                      style={inviteCode}
                      title={t("household.tap_to_copy")}
                    >
                      {inv.code}
                    </button>
                    <span
                      style={{
                        fontSize: "0.75rem",
                        color: "var(--color-text-muted)",
                        marginLeft: "auto",
                      }}
                    >
                      {t("household.expires", {
                        date: new Date(inv.expires_at).toLocaleDateString(),
                      })}
                    </span>
                    <Button variant="secondary" size="sm" onClick={() => setRevokeTarget(inv.code)}>
                      {t("household.revoke")}
                    </Button>
                  </li>
                ))}
              </ul>
            </>
          )}
        </section>
      )}

      <section style={card}>
        <button
          onClick={() => setAuditOpen((v) => !v)}
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            width: "100%",
            background: "transparent",
            border: "none",
            color: "var(--color-text)",
            padding: 0,
            cursor: "pointer",
          }}
          aria-expanded={auditOpen}
        >
          <h3 style={cardTitle}>
            {t("household.activity_title")} · {audit.length}
          </h3>
          <Icon
            name="chevron-down"
            size={18}
            style={{
              transform: auditOpen ? "rotate(180deg)" : "none",
              transition: "transform 120ms",
            }}
          />
        </button>
        {auditOpen && (
          <div style={{ marginTop: "0.75rem" }}>
            <AuditList entries={audit} />
          </div>
        )}
      </section>

      <section style={card}>
        <h3 style={cardTitle}>{t("household.leave_title")}</h3>
        <p style={cardDesc}>{t("household.leave_description")}</p>
        <Button
          variant="danger"
          onClick={() => setConfirmLeave(true)}
          iconLeft={<Icon name="logout" size={16} />}
        >
          {t("household.leave_button")}
        </Button>
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

      <ConfirmDialog
        open={revokeTarget !== null}
        title={t("household.revoke_confirm_title")}
        description={t("household.revoke_confirm_description")}
        confirmLabel={t("household.revoke")}
        destructive
        onConfirm={confirmRevoke}
        onCancel={() => setRevokeTarget(null)}
      />

      <ConfirmDialog
        open={transferTarget !== null}
        title={
          transferTarget ? t("household.transfer_confirm_title", { name: transferTarget.name }) : ""
        }
        description={t("household.transfer_confirm_description")}
        confirmLabel={t("household.transfer_button")}
        onConfirm={confirmTransfer}
        onCancel={() => setTransferTarget(null)}
      />

      <ConfirmDialog
        open={confirmLeave}
        title={t("household.leave_confirm_title")}
        description={t("household.leave_confirm_description")}
        confirmLabel={t("household.leave_button")}
        destructive
        onConfirm={handleLeave}
        onCancel={() => setConfirmLeave(false)}
      />
    </div>
  );
}

// ActiveInviteBlock renders the most-recently-generated invite with the
// QR-code toggle. Lives inside Household so it can reach `t` and styles.
function ActiveInviteBlock({
  invite,
  showQr,
  onToggleQr,
  onCopy,
}: {
  invite: Invite;
  showQr: boolean;
  onToggleQr: () => void;
  onCopy: (code: string) => void;
}) {
  const { t } = useTranslation();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  // Generate QR whenever the active code changes or the panel is opened.
  // Encode a deeplink so a phone scanning it lands directly on the Join
  // page with the code prefilled — no manual typing.
  useEffect(() => {
    if (!showQr || !canvasRef.current) return;
    const url = `${window.location.origin}/household/join?code=${invite.code}`;
    QRCode.toCanvas(canvasRef.current, url, { width: 220, margin: 1 }).catch(() => {
      // Generation failure is silent — user can still copy the code.
    });
  }, [showQr, invite.code]);

  return (
    <div style={codeBlockWrap}>
      <button
        onClick={() => onCopy(invite.code)}
        style={codeBlock}
        title={t("household.tap_to_copy")}
      >
        <span style={{ fontSize: "1.6rem", letterSpacing: "0.18em", fontWeight: 700 }}>
          {invite.code}
        </span>
        <span
          style={{ fontSize: "0.8rem", color: "var(--color-text-muted)", marginTop: "0.25rem" }}
        >
          {t("household.tap_to_copy")} ·{" "}
          {t("household.expires", { date: new Date(invite.expires_at).toLocaleDateString() })}
        </span>
      </button>
      <button onClick={onToggleQr} style={qrToggle}>
        {showQr ? t("household.hide_qr") : t("household.show_qr")}
      </button>
      {showQr && (
        <div style={qrFrame}>
          <canvas ref={canvasRef} aria-label={t("household.qr_alt")} />
        </div>
      )}
    </div>
  );
}

// --- styles ---

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
  background: "var(--color-primary)",
  color: "#fff",
  padding: "0.1rem 0.4rem",
  borderRadius: 4,
  textTransform: "uppercase",
  letterSpacing: "0.05em",
};
const codeBlockWrap: React.CSSProperties = { marginTop: "1rem" };
const codeBlock: React.CSSProperties = {
  width: "100%",
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  background: "var(--color-bg)",
  border: "1px dashed var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "1rem",
  cursor: "pointer",
};
const qrToggle: React.CSSProperties = {
  width: "100%",
  marginTop: "0.5rem",
  padding: "0.5rem",
  background: "transparent",
  color: "var(--color-text-muted)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  fontSize: "0.85rem",
  cursor: "pointer",
};
const qrFrame: React.CSSProperties = {
  display: "flex",
  justifyContent: "center",
  background: "#fff",
  marginTop: "0.5rem",
  padding: "0.75rem",
  borderRadius: "var(--radius)",
};
const inviteRow: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: "0.5rem",
  padding: "0.4rem 0",
  borderBottom: "1px solid var(--color-border)",
};
const inviteCode: React.CSSProperties = {
  fontFamily: "ui-monospace, monospace",
  fontSize: "0.95rem",
  letterSpacing: "0.1em",
  background: "var(--color-bg)",
  padding: "0.25rem 0.5rem",
  borderRadius: "var(--radius)",
  border: "1px solid var(--color-border)",
  color: "var(--color-text)",
  cursor: "pointer",
};
const transferRow: React.CSSProperties = {
  display: "flex",
  justifyContent: "space-between",
  alignItems: "center",
  width: "100%",
  background: "var(--color-bg)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.65rem 0.85rem",
  color: "var(--color-text)",
  cursor: "pointer",
};
const iconBtn: React.CSSProperties = {
  width: 32,
  height: 32,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  borderRadius: "var(--radius)",
  color: "var(--color-text-muted)",
  background: "transparent",
  border: "none",
  cursor: "pointer",
};
const nameInput: React.CSSProperties = {
  flex: 1,
  background: "var(--color-bg)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.5rem 0.7rem",
  color: "var(--color-text)",
  fontSize: "1rem",
};
