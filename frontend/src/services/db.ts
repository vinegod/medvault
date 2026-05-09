import Dexie, { type Table } from "dexie";
import type { LocalMedicine, MedVaultExport, MedicineInput } from "../types";

class MedVaultDB extends Dexie {
  medicines!: Table<LocalMedicine>;
  meta!: Table<{ key: string; value: string }>;

  constructor() {
    super("MedVault");

    this.version(1).stores({
      medicines: "id, name, category, expiration_date, updated_at, sync_status, deleted_at",
      meta: "key", // stores last_sync, server_url, auth tokens, user settings
    });
  }
}

export const db = new MedVaultDB();

// ── Meta helpers ──────────────────────────────────────────────

export async function getMeta(key: string): Promise<string | null> {
  const record = await db.meta.get(key);
  return record?.value ?? null;
}

export async function setMeta(key: string, value: string): Promise<void> {
  await db.meta.put({ key, value });
}

export async function deleteMeta(key: string): Promise<void> {
  await db.meta.delete(key);
}

// ── Medicine CRUD (primary operations — always local) ─────────

export async function addMedicine(input: MedicineInput): Promise<LocalMedicine> {
  const now = new Date().toISOString();
  const isServerConnected = await getMeta("server_url");

  const medicine: LocalMedicine = {
    id: crypto.randomUUID(),
    ...input,
    photo_hash: null,
    created_at: now,
    updated_at: now,
    deleted_at: null,
    sync_status: isServerConnected ? "pending" : "local",
  };

  await db.medicines.add(medicine);
  return medicine;
}

export async function updateMedicine(id: string, changes: Partial<MedicineInput>): Promise<void> {
  const now = new Date().toISOString();
  const isServerConnected = await getMeta("server_url");

  await db.medicines.update(id, {
    ...changes,
    updated_at: now,
    sync_status: isServerConnected ? "pending" : "local",
  });
}

// Quick quantity adjustment — used by the +/- buttons on MedicineCard.
// Returns the new quantity (clamped to >= 0).
export async function adjustQuantity(id: string, delta: number): Promise<number> {
  const m = await db.medicines.get(id);
  if (!m) return 0;
  const newQty = Math.max(0, m.quantity + delta);
  await updateMedicine(id, { quantity: newQty });
  return newQty;
}

export async function deleteMedicine(id: string): Promise<void> {
  const now = new Date().toISOString();
  const isServerConnected = await getMeta("server_url");

  // Soft delete — needed for sync. In standalone mode we could hard-delete,
  // but keeping it consistent simplifies the code.
  await db.medicines.update(id, {
    deleted_at: now,
    updated_at: now,
    sync_status: isServerConnected ? "pending" : "local",
  });
}

// restoreMedicine clears the soft-delete flag — used by the toast "Undo"
// action right after a delete. Bumps updated_at so the un-delete propagates
// to the server on the next sync.
export async function restoreMedicine(id: string): Promise<void> {
  const now = new Date().toISOString();
  const isServerConnected = await getMeta("server_url");
  await db.medicines.update(id, {
    deleted_at: null,
    updated_at: now,
    sync_status: isServerConnected ? "pending" : "local",
  });
}

// ── Queries ───────────────────────────────────────────────────

export async function getActiveMedicines(): Promise<LocalMedicine[]> {
  return db.medicines.filter((m) => m.deleted_at === null).sortBy("name");
}

export async function getMedicineById(id: string): Promise<LocalMedicine | undefined> {
  return db.medicines.get(id);
}

export async function getExpiringMedicines(withinDays: number): Promise<LocalMedicine[]> {
  const now = new Date();
  const cutoff = new Date(now.getTime() + withinDays * 24 * 60 * 60 * 1000);
  const cutoffStr = cutoff.toISOString().split("T")[0];

  return db.medicines
    .filter(
      (m) => m.deleted_at === null && m.expiration_date !== null && m.expiration_date <= cutoffStr
    )
    .sortBy("expiration_date");
}

// getLowStockMedicines returns active medicines at or below the threshold
// (0 included). Sorted ascending so the most-empty appears first.
export async function getLowStockMedicines(threshold: number): Promise<LocalMedicine[]> {
  const list = await db.medicines
    .filter((m) => m.deleted_at === null && m.quantity <= threshold)
    .toArray();
  return list.sort((a, b) => a.quantity - b.quantity);
}

// getDistinctCategories returns sorted unique non-empty category strings —
// used by the form's <datalist> so users see existing categories as
// suggestions instead of accidentally creating "Vitamin" / "vitamins".
export async function getDistinctCategories(): Promise<string[]> {
  const list = await db.medicines.filter((m) => m.deleted_at === null).toArray();
  const set = new Set<string>();
  for (const m of list) {
    const c = m.category?.trim();
    if (c) set.add(c);
  }
  return Array.from(set).sort((a, b) => a.localeCompare(b));
}

// ── Sync helpers (used by sync.ts when server is connected) ───

export async function getPendingChanges(): Promise<LocalMedicine[]> {
  return db.medicines.where("sync_status").equals("pending").toArray();
}

// ── Export / Import (manual backup — works without server) ────

export async function exportAll(): Promise<string> {
  const medicines = await db.medicines.toArray();
  const metaRecords = await db.meta.toArray();
  const settings: Record<string, string> = {};
  for (const m of metaRecords) {
    // Don't export auth tokens or server config
    if (!["access_token", "refresh_token", "server_url"].includes(m.key)) {
      settings[m.key] = m.value;
    }
  }

  const exportData: MedVaultExport = {
    version: 1,
    exported_at: new Date().toISOString(),
    medicines: medicines.map(({ sync_status: _, ...rest }) => rest),
    settings,
  };

  return JSON.stringify(exportData, null, 2);
}

export async function importData(json: string): Promise<number> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(json);
  } catch {
    throw new Error("Invalid JSON");
  }
  const data = parsed as MedVaultExport;
  if (!Array.isArray(data?.medicines)) {
    throw new Error("Missing medicines array");
  }
  let importedCount = 0;

  await db.transaction("rw", db.medicines, async () => {
    for (const medicine of data.medicines) {
      const existing = await db.medicines.get(medicine.id);
      if (!existing || medicine.updated_at > existing.updated_at) {
        const isServerConnected = await getMeta("server_url");
        await db.medicines.put({
          ...medicine,
          sync_status: isServerConnected ? "pending" : "local",
        });
        importedCount++;
      }
    }
  });

  return importedCount;
}
