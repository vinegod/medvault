import { effectiveExpiration } from "./effectiveExpiration";
import type { Medicine } from "../types";

type Translate = (key: string) => string;

const COLUMNS: { header: string; value: (m: Medicine, t: Translate) => string | number | null }[] =
  [
    { header: "medicine_form.name", value: (m) => m.name },
    { header: "medicine_form.active_ingredient", value: (m) => m.active_ingredient },
    { header: "medicine_form.quantity", value: (m) => m.quantity },
    { header: "medicine_form.unit", value: (m, t) => t(`units.${m.unit}`) },
    { header: "medicine_form.category", value: (m) => m.category },
    { header: "medicine_form.expiration_date", value: (m) => m.expiration_date },
    { header: "medicine_form.opened_at", value: (m) => m.opened_at },
    {
      header: "inventory.use_by",
      value: (m) => effectiveExpiration(m.expiration_date, m.opened_at, m.days_after_opening),
    },
    { header: "medicine_form.storage_location", value: (m) => m.storage_location },
    { header: "medicine_form.barcode", value: (m) => m.barcode },
    { header: "medicine_form.notes", value: (m) => m.notes },
  ];

// medicinesToCSV renders medicines as RFC 4180 CSV with localized headers.
// The leading BOM makes spreadsheet apps read the file as UTF-8.
export function medicinesToCSV(list: Medicine[], t: Translate): string {
  const rows = [COLUMNS.map((c) => t(c.header))];
  for (const m of list) rows.push(COLUMNS.map((c) => String(c.value(m, t) ?? "")));
  return "﻿" + rows.map((r) => r.map(csvField).join(",")).join("\r\n") + "\r\n";
}

function csvField(s: string): string {
  return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}
