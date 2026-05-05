// Local-only expiration-warning notifications. Uses the PWA Periodic
// Background Sync API where supported (Chrome/Edge) so the service worker
// can re-check the IndexedDB cache once a day even when the app isn't
// open. On Safari/Firefox we fall back to checking on app focus and
// surfacing an in-app banner instead.
//
// No push, no VAPID keys, no server involvement: this aligns with the
// project's self-hosted, no-tracking philosophy.

const PERIODIC_TAG = "medvault-expiry-check";
const STORAGE_KEY = "notifications_enabled";

// Whether the browser exposes the Periodic Background Sync API. Chromium
// gated this behind PWA install + permission.
type PeriodicSyncManager = {
  register(tag: string, options?: { minInterval: number }): Promise<void>;
  unregister(tag: string): Promise<void>;
  getTags(): Promise<string[]>;
};
type SWRegistrationWithSync = ServiceWorkerRegistration & {
  periodicSync?: PeriodicSyncManager;
};

export function isPeriodicSyncSupported(): boolean {
  return (
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator &&
    "PeriodicSyncManager" in window
  );
}

export function notificationPermission(): NotificationPermission {
  if (typeof Notification === "undefined") return "denied";
  return Notification.permission;
}

// requestPermissionAndRegister is the user-facing entry point: show the
// browser permission prompt, then (on grant) register the periodic sync.
// Returns true on success, false if anything was blocked or unavailable.
export async function requestPermissionAndRegister(): Promise<boolean> {
  if (typeof Notification === "undefined") return false;

  const perm = await Notification.requestPermission();
  if (perm !== "granted") {
    localStorage.removeItem(STORAGE_KEY);
    return false;
  }

  if (isPeriodicSyncSupported()) {
    try {
      const reg = (await navigator.serviceWorker.ready) as SWRegistrationWithSync;
      // The browser can refuse the registration silently (PWA not installed,
      // user has no engagement, etc.). We don't treat that as a hard error
      // — the in-app fallback (see runFocusFallback) still works.
      await reg.periodicSync?.register(PERIODIC_TAG, {
        minInterval: 24 * 60 * 60 * 1000, // 1 day
      });
    } catch (err) {
      console.warn("Periodic sync registration failed:", err);
    }
  }

  localStorage.setItem(STORAGE_KEY, "1");
  return true;
}

export async function disable(): Promise<void> {
  localStorage.removeItem(STORAGE_KEY);
  if (isPeriodicSyncSupported()) {
    try {
      const reg = (await navigator.serviceWorker.ready) as SWRegistrationWithSync;
      await reg.periodicSync?.unregister(PERIODIC_TAG);
    } catch {
      // ignore
    }
  }
}

export function isEnabled(): boolean {
  return localStorage.getItem(STORAGE_KEY) === "1" && notificationPermission() === "granted";
}
