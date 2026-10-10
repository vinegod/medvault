import i18n from "../i18n";
import { db, getMeta, setMeta, deleteMeta } from "./db";
import { apiClient } from "./api";
import { performSync, initSyncListeners, stopSyncListeners } from "./sync";
import type { ServerConnectionStatus, AuthResponse } from "../types";

type StatusListener = (status: ServerConnectionStatus) => void;

let currentStatus: ServerConnectionStatus = "disconnected";
const listeners: Set<StatusListener> = new Set();

function setStatus(status: ServerConnectionStatus): void {
  currentStatus = status;
  listeners.forEach((fn) => fn(status));
}

export function onServerStatusChange(fn: StatusListener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function getServerStatus(): ServerConnectionStatus {
  return currentStatus;
}

// Check if a server is configured and try to restore connection on app start
export async function initServerConnection(): Promise<void> {
  const serverUrl = await getMeta("server_url");
  if (!serverUrl) {
    setStatus("disconnected");
    return;
  }

  // Server was previously configured — try to reconnect
  apiClient.setBaseUrl(serverUrl);
  const refreshToken = await getMeta("refresh_token");
  if (refreshToken) {
    setStatus("connecting");
    try {
      const healthy = await checkServerHealth(serverUrl);
      if (healthy) {
        setStatus("connected");
        initSyncListeners();
        performSync();
      } else {
        setStatus("error");
      }
    } catch {
      setStatus("error");
    }
  } else {
    // URL saved but no auth — need to log in again
    setStatus("disconnected");
  }
}

// Verify server is reachable and compatible
export async function checkServerHealth(url: string): Promise<boolean> {
  try {
    const response = await fetch(`${url}/api/health`, {
      method: "GET",
      signal: AbortSignal.timeout(5000),
    });
    if (!response.ok) return false;
    const data = await response.json();
    return data?.data?.status === "ok";
  } catch {
    return false;
  }
}

// Connect to a server for the first time. inviteCode is only sent on
// register; it joins the invited household instead of creating one.
export async function connectToServer(
  url: string,
  username: string,
  password: string,
  isRegister: boolean,
  inviteCode = ""
): Promise<{ success: boolean; error?: string }> {
  setStatus("connecting");

  // Verify server health
  const healthy = await checkServerHealth(url);
  if (!healthy) {
    setStatus("disconnected");
    // Translate at the source — callers display result.error verbatim.
    return { success: false, error: i18n.t("auth.server_unreachable") };
  }

  // Register or login
  const endpoint = isRegister ? "/api/auth/register" : "/api/auth/login";
  try {
    const response = await fetch(`${url}${endpoint}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(
        isRegister && inviteCode
          ? { username, password, invite_code: inviteCode }
          : { username, password }
      ),
    });

    const result = await response.json();
    if (result.error) {
      setStatus("disconnected");
      return {
        success: false,
        error: i18n.t(`auth.errors.${result.error.code}`, { defaultValue: result.error.message }),
      };
    }

    const auth: AuthResponse = result.data;

    // Store connection info
    await setMeta("server_url", url);
    await setMeta("refresh_token", auth.refresh_token);
    await setMeta("user_id", auth.user.id);
    await setMeta("username", auth.user.username);

    // Configure API client
    apiClient.setBaseUrl(url);
    apiClient.setAccessToken(auth.access_token);

    // Mark all local records as pending so they sync up
    await db.medicines.where("sync_status").equals("local").modify({ sync_status: "pending" });

    setStatus("connected");

    // Start sync
    initSyncListeners();
    await performSync();

    return { success: true };
  } catch {
    setStatus("disconnected");
    return { success: false, error: "Connection failed" };
  }
}

// Disconnect from server. Best-effort revokes the refresh token server-side
// before clearing local credentials, then wipes all server-tracked records
// so they can't be re-uploaded to a different server on the next connect.
//
// Records the user created while standalone (sync_status === "local" before
// any connect) are preserved.
export async function disconnectFromServer(): Promise<void> {
  stopSyncListeners();

  // Tell the server to revoke the refresh token. If this fails (server
  // down, network gone), the token remains valid until its TTL — but at
  // least we tried, and the local copy is gone in a moment.
  const refreshToken = await getMeta("refresh_token");
  if (refreshToken) {
    try {
      await apiClient.post("/api/auth/logout", { refresh_token: refreshToken });
    } catch {
      // Network failure is OK — local cleanup still proceeds.
    }
  }

  // Delete every record that ever touched this server (synced, pending,
  // conflict). Without this, reconnecting to a different server would
  // upload these as if they belonged to the new household. The "local"
  // label is reserved for records created while no server was configured;
  // we never re-label here, so there's no risk of mid-flight records being
  // mistaken for standalone ones on the next connect.
  await db.medicines.where("sync_status").notEqual("local").delete();

  await deleteMeta("server_url");
  await deleteMeta("refresh_token");
  await deleteMeta("user_id");
  await deleteMeta("username");
  await deleteMeta("last_sync");

  apiClient.setBaseUrl("");
  apiClient.setAccessToken(null);

  setStatus("disconnected");
}

// Check if server is currently configured
export async function isServerConfigured(): Promise<boolean> {
  const url = await getMeta("server_url");
  return url !== null;
}
