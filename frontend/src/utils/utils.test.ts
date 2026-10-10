import { describe, expect, it } from "vitest";
import { ingredientTokens, sharedIngredient } from "./ingredients";
import { groupMedicines } from "./groups";
import { medicinesToCSV } from "./csv";
import { expiryBucket } from "./effectiveExpiration";
import type { LocalMedicine } from "../types";

function med(over: Partial<LocalMedicine>): LocalMedicine {
  return {
    id: crypto.randomUUID(),
    name: "Ibuprofen",
    quantity: 10,
    unit: "tablets",
    category: "",
    active_ingredient: null,
    expiration_date: null,
    opened_at: null,
    days_after_opening: null,
    storage_location: "",
    barcode: "",
    notes: "",
    photo_hash: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    deleted_at: null,
    disposed_at: null,
    sync_status: "local",
    ...over,
  };
}

describe("ingredients", () => {
  it("splits and normalises combinations", () => {
    expect(ingredientTokens(" Paracetamol + Caffeine; ASPIRIN ")).toEqual([
      "paracetamol",
      "caffeine",
      "aspirin",
    ]);
    expect(ingredientTokens(null)).toEqual([]);
  });

  it("detects a shared ingredient", () => {
    expect(sharedIngredient("Paracetamol, Caffeine", "paracetamol")).toBe(true);
    expect(sharedIngredient("Ibuprofen", "Paracetamol")).toBe(false);
    expect(sharedIngredient("", "")).toBe(false);
  });
});

describe("groupMedicines", () => {
  it("groups packs by name and unit with aggregates", () => {
    const groups = groupMedicines([
      med({ name: "Ibuprofen", quantity: 10, expiration_date: "2027-05-01" }),
      med({ name: "ibuprofen ", quantity: 4, expiration_date: "2026-12-01" }),
      med({ name: "Ibuprofen", unit: "ml", quantity: 100 }),
    ]);
    expect(groups).toHaveLength(2);
    expect(groups[0].packs).toHaveLength(2);
    expect(groups[0].totalQuantity).toBe(14);
    expect(groups[0].earliestExpiration).toBe("2026-12-01");
  });
});

describe("medicinesToCSV", () => {
  it("quotes fields and starts with a BOM", () => {
    const csv = medicinesToCSV([med({ name: 'Say "ah"', notes: "a,b\nc" })], (k) => k);
    expect(csv.startsWith("﻿")).toBe(true);
    const lines = csv.slice(1).split("\r\n");
    expect(lines[0].split(",")[0]).toBe("medicine_form.name");
    expect(lines[1]).toContain('"Say ""ah"""');
    expect(csv).toContain('"a,b\nc"');
  });

  it("neutralises cells that start like a formula", () => {
    const csv = medicinesToCSV(
      [med({ name: '=HYPERLINK("http://x")', notes: "@SUM(A1)", storage_location: "+1" })],
      (k) => k
    );
    const row = csv.slice(1).split("\r\n")[1];
    expect(row.startsWith(`"'=HYPERLINK(""http://x"")"`)).toBe(true);
    expect(row).toContain("'@SUM(A1)");
    expect(row).toContain("'+1");
  });
});

describe("expiryBucket", () => {
  const today = "2026-10-10";
  it.each([
    [null, "none"],
    ["2026-10-09", "expired"],
    ["2026-10-10", "soon"],
    ["2026-11-09", "soon"],
    ["2026-12-01", "upcoming"],
    ["2027-06-01", "ok"],
  ])("%s -> %s", (date, want) => {
    expect(
      expiryBucket({ expiration_date: date, opened_at: null, days_after_opening: null }, today)
    ).toBe(want);
  });

  it("uses the post-opening date when earlier", () => {
    expect(
      expiryBucket(
        { expiration_date: "2028-01-01", opened_at: "2026-09-01", days_after_opening: 28 },
        today
      )
    ).toBe("expired");
  });
});
