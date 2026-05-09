import { db, getMeta, setMeta, deleteMeta, getPendingChanges } from "./db";
import { apiClient } from "./api";
import type { Medicine, SyncResponse } from "../types";

// Tunables. Pulled out so the cadence is reviewable in one place.
const SYNC_DEBOUNCE_MS = 5_000;
const SYNC_INTERVAL_MS = 5 * 60 * 1000;

// rebuildLocalCacheFromServer wipes the local medicine cache and forces a
// full re-sync. Used by Join and Switch household flows — both swap the
// server-side scope (household_id) and the cache becomes meaningless.
export async function rebuildLocalCacheFromServer(): Promise<void> {
  await db.medicines.clear();
  await deleteMeta("last_sync");
  await performSync();
}

let onSyncError: ((err: Error) => void) | null = null;

export function setSyncErrorHandler(fn: (err: Error) => void): void {
  onSyncError = fn;
}

let syncInProgress = false;
let syncTimer: ReturnType<typeof setInterval> | null = null;
let debounceTimer: ReturnType<typeof setTimeout> | null = null;
let listenersActive = false;

// ── Public API ────────────────────────────────────────────────

// Called after a local write — debounces before syncing
export function scheduleSyncAfterWrite(): void {
  if (!listenersActive) return; // No server connected
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => performSync(), SYNC_DEBOUNCE_MS);
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
    // Snapshot the IDs we're about to send. Anything that becomes pending
    // *after* this point (a user edit during the network round-trip) must
    // not be flipped to "synced" — it hasn't actually been sent yet.
    const sentIds = pendingChanges.map((m) => m.id);

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

    // Only flip the IDs we actually sent. Records that became pending
    // mid-round-trip stay pending so the next sync picks them up.
    if (sentIds.length > 0) {
      await db.medicines
        .where("id")
        .anyOf(sentIds)
        .filter((m) => m.sync_status === "pending")
        .modify({ sync_status: "synced" });
    }

    await setMeta("last_sync", response.data.server_time);
  } catch (error) {
    console.error("Sync failed:", error);
    onSyncError?.(error instanceof Error ? error : new Error(String(error)));
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

  syncTimer = setInterval(() => performSync(), SYNC_INTERVAL_MS);

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
