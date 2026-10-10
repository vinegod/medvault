import { effectiveExpiration } from "./effectiveExpiration";
import type { LocalMedicine } from "../types";

// MedicineGroup is every pack of one medicine: same name (case-insensitive)
// and same unit, so quantities can be summed.
export interface MedicineGroup {
  key: string;
  packs: LocalMedicine[];
  totalQuantity: number;
  earliestExpiration: string | null;
}

// groupMedicines groups packs by name and unit. Groups keep the position of
// their first pack in list, so a sorted list yields sorted groups.
export function groupMedicines(list: LocalMedicine[]): MedicineGroup[] {
  const groups = new Map<string, MedicineGroup>();
  for (const m of list) {
    const key = `${m.name.trim().toLowerCase()}|${m.unit}`;
    const eff = effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening);
    const g = groups.get(key);
    if (!g) {
      groups.set(key, {
        key,
        packs: [m],
        totalQuantity: m.quantity,
        earliestExpiration: eff,
      });
      continue;
    }
    g.packs.push(m);
    g.totalQuantity += m.quantity;
    if (eff && (!g.earliestExpiration || eff < g.earliestExpiration)) g.earliestExpiration = eff;
  }
  return [...groups.values()];
}
