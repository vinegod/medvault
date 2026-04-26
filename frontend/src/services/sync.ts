import { db, getMeta, setMeta, getPendingChanges } from "./db";
import { apiClient } from "./api";
import type { Medicine, SyncResponse } from "../types";

let syncInProgress = false;
let syncTimer: ReturnType<typeof setInterval> | null = null;
let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let listenersActive = false;

// ── Public API ────────────────────────────────────────────────

// Called after a local write — debounces before syncing
export function scheduleSyncAfterWrite(): void {
  if (!listenersActive) return; // No server connected
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => performSync(), 5000);
}

// Main sync function — safe to call anytime; no-ops if not connected
export async function performSync(): Promise<void> {
  if (syncInProgress) return;
  if (!navigator.onLine) return;

  const serverUrl = await getMeta("server_url");
  if (!serverUrl) return; // No server configured — do nothing

  syncInProgress = true;
  try {
    const lastSync = await getMeta("last_sync");
    const pendingChanges = await getPendingChanges();

    // Strip client-only fields before sending
    const changes: Medicine[] = pendingChanges.map(({ sync_status: _, ...medicine }) => medicine);

    const response = await apiClient.post<SyncResponse>("/api/sync", {
      last_sync: lastSync,
      changes,
    });

    if (!response.data) return;

    // Merge server records into local DB
    await db.transaction("rw", db.medicines, async () => {
      for (const serverRecord of response.data!.records) {
        const local = await db.medicines.get(serverRecord.id);

        if (!local || serverRecord.updated_at > local.updated_at) {
          await db.medicines.put({
            ...serverRecord,
            sync_status: "synced",
          });
        }
      }
    });

    // Mark all pending as synced
    await db.medicines.where("sync_status").equals("pending").modify({ sync_status: "synced" });

    // Update last sync timestamp
    await setMeta("last_sync", response.data.server_time);
  } catch (error) {
    console.error("Sync failed:", error);
    // Will retry on next trigger — app continues working normally
  } finally {
    syncInProgress = false;
  }
}

// ── Lifecycle ─────────────────────────────────────────────────

function onOnline(): void {
  performSync();
}

function onVisible(): void {
  if (document.visibilityState === "visible") performSync();
}

export function initSyncListeners(): void {
  if (listenersActive) return;

  window.addEventListener("online", onOnline);
  document.addEventListener("visibilitychange", onVisible);

  // Periodic sync every 5 minutes
  syncTimer = setInterval(() => performSync(), 5 * 60 * 1000);

  listenersActive = true;

  // Initial sync
  performSync();
}

export function stopSyncListeners(): void {
  if (!listenersActive) return;

  window.removeEventListener("online", onOnline);
  document.removeEventListener("visibilitychange", onVisible);

  if (syncTimer) {
    clearInterval(syncTimer);
    syncTimer = null;
  }
  if (debounceTimer) {
    clearTimeout(debounceTimer);
    debounceTimer = null;
  }

  listenersActive = false;
}
