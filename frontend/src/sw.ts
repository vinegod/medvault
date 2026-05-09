/// <reference lib="webworker" />
//
// Custom service worker. Injected into the build by vite-plugin-pwa
// (strategies: 'injectManifest'). Two responsibilities:
//
//   1. Precache the app shell so the PWA boots offline.
//   2. Handle the daily 'periodicsync' tick to check IndexedDB for
//      medicines expiring soon and fire a local notification. No push,
//      no server involvement.
//
// The IndexedDB read is done via raw IDBObjectStore APIs (not Dexie) to
// keep the SW bundle small and to avoid pulling React-tree code into the
// SW build.

import { precacheAndRoute, cleanupOutdatedCaches } from "workbox-precaching";

declare const self: ServiceWorkerGlobalScope;

precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();

// activate immediately so periodic sync registrations bind to the new SW
// rather than the previous version.
self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

const PERIODIC_TAG = "medvault-expiry-check";
const NOTIFY_WITHIN_DAYS = 7;

interface PeriodicSyncEvent extends ExtendableEvent {
  tag: string;
}

self.addEventListener("periodicsync", ((event: PeriodicSyncEvent) => {
  if (event.tag !== PERIODIC_TAG) return;
  event.waitUntil(checkExpirations());
}) as EventListener);

async function checkExpirations(): Promise<void> {
  if (typeof Notification === "undefined" || Notification.permission !== "granted") {
    return;
  }

  let db: IDBDatabase;
  try {
    db = await openMedicineDB();
  } catch (err) {
    console.warn("expiry-check: open IDB failed", err);
    return;
  }

  let expiring: number;
  try {
    expiring = await countExpiring(db, NOTIFY_WITHIN_DAYS);
  } catch (err) {
    console.warn("expiry-check: read failed", err);
    return;
  } finally {
    db.close();
  }

  if (expiring === 0) return;

  // Re-firing the same `tag` replaces the existing notification rather
  // than stacking — important for repeat daily checks.
  await self.registration.showNotification("MedVault", {
    body:
      expiring === 1
        ? "1 medicine expires within the next week"
        : `${expiring} medicines expire within the next week`,
    icon: "/icons/icon-192.png",
    tag: "medvault-expiry",
  });
}

function openMedicineDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open("MedVault", 1);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

interface MedicineRow {
  deleted_at: string | null;
  expiration_date: string | null;
  opened_at: string | null;
  days_after_opening: number | null;
}

// countExpiring scans the medicines store and returns how many active
// records will expire within `days` from now. Mirrors the
// effectiveExpiration helper used in the UI.
function countExpiring(db: IDBDatabase, days: number): Promise<number> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction("medicines", "readonly");
    const store = tx.objectStore("medicines");
    const req = store.getAll();

    req.onsuccess = () => {
      const today = new Date();
      const todayStr = isoDate(today);
      const cutoff = isoDate(new Date(today.getTime() + days * 86_400_000));
      let count = 0;

      for (const row of req.result as MedicineRow[]) {
        if (row.deleted_at !== null) continue;
        const eff = effective(row.expiration_date, row.opened_at, row.days_after_opening);
        if (!eff) continue;
        if (eff >= todayStr && eff <= cutoff) count++;
      }
      resolve(count);
    };
    req.onerror = () => reject(req.error);
  });
}

// effective is a tiny copy of utils/effectiveExpiration.ts. Duplicated here
// rather than imported so the SW bundle stays self-contained — the React
// app utilities live in a separate chunk we don't want to drag into the SW.
function effective(
  expirationDate: string | null,
  openedAt: string | null,
  dao: number | null
): string | null {
  const printed = expirationDate ?? null;
  const opened = openedAt && dao ? addDays(openedAt, dao) : null;
  if (printed && opened) return printed < opened ? printed : opened;
  return printed ?? opened;
}

function addDays(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const t = Date.UTC(y, m - 1, d) + days * 86_400_000;
  return isoDate(new Date(t));
}

function isoDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}
