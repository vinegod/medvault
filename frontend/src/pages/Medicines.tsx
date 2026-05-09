import { useState, useMemo, useEffect } from "react";
import { useLiveQuery } from "dexie-react-hooks";
import { useTranslation } from "react-i18next";
import Fuse from "fuse.js";
import {
  db,
  deleteMedicine,
  restoreMedicine,
  adjustQuantity,
  getMeta,
  setMeta,
} from "../services/db";
import { MedicineCard } from "../components/MedicineCard";
import { SearchBar } from "../components/SearchBar";
import { SortBar, type SortKey, isSortKey } from "../components/SortBar";
import { EmptyState } from "../components/EmptyState";
import { FAB } from "../components/FAB";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { useToast } from "../contexts/ToastContext";
import { Link } from "react-router-dom";
import type { LocalMedicine } from "../types";
import { Skeleton } from "../components/Skeleton";
import { DEFAULT_LOW_STOCK } from "../constants";

const SORT_META_KEY = "medicines_sort";

export default function Medicines() {
  const [search, setSearch] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("name");
  const [lowStockThreshold, setLowStockThreshold] = useState(DEFAULT_LOW_STOCK);
  const toast = useToast();
  const { t } = useTranslation();

  const medicines = useLiveQuery(
    () => db.medicines.filter((m) => m.deleted_at === null).toArray(),
    []
  );

  // Persist sort choice — read on mount, write on change. Not reactive
  // because no other view uses this preference.
  useEffect(() => {
    getMeta(SORT_META_KEY).then((v) => {
      if (v && isSortKey(v)) setSortKey(v);
    });
    getMeta("low_stock_threshold").then((v) => {
      const n = v ? Number.parseInt(v, 10) : DEFAULT_LOW_STOCK;
      if (!Number.isNaN(n)) setLowStockThreshold(n);
    });
  }, []);

  function changeSort(key: SortKey) {
    setSortKey(key);
    // Fire-and-forget is intentional here: the UI shouldn't wait on
    // IndexedDB to render the new sort. void marks intent for the linter.
    void setMeta(SORT_META_KEY, key);
  }

  const fuse = useMemo(
    () =>
      medicines
        ? new Fuse(medicines, {
            keys: [
              { name: "name", weight: 0.5 },
              { name: "category", weight: 0.3 },
              { name: "storage_location", weight: 0.15 },
              { name: "notes", weight: 0.05 },
            ],
            threshold: 0.4,
            ignoreLocation: true,
          })
        : null,
    [medicines]
  );

  // When the user types a query, results are ordered by Fuse relevance.
  // When idle, the chosen sort takes over.
  const filtered = useMemo(() => {
    if (!medicines) return [];
    if (search.trim()) return fuse!.search(search).map((r) => r.item);
    return [...medicines].sort(comparator(sortKey));
  }, [fuse, medicines, search, sortKey]);

  // Names that appear on more than one card after filtering — used to
  // surface a fallback subtitle so duplicates aren't visually identical
  // when category is empty.
  const ambiguousNames = useMemo(() => {
    const counts = new Map<string, number>();
    for (const m of filtered) counts.set(m.name, (counts.get(m.name) ?? 0) + 1);
    return new Set([...counts].filter(([, c]) => c > 1).map(([n]) => n));
  }, [filtered]);

  // Stable wall-clock reference for staleness checks in cards. Captured
  // once per render in the parent so MedicineCard's render stays pure.
  const [now] = useState(() => Date.now());

  async function handleAdjust(id: string, delta: number) {
    const newQty = await adjustQuantity(id, delta);
    if (delta < 0 && newQty === 0) {
      toast.info(t("medicines.out_of_stock"));
    }
  }

  async function handleDelete(id: string, name: string) {
    await deleteMedicine(id);
    toast.success(t("medicines.deleted", { name }), {
      actionLabel: t("common.undo"),
      onAction: () => {
        restoreMedicine(id);
      },
    });
  }

  return (
    <>
      <div style={{ marginBottom: "0.75rem" }}>
        <SearchBar
          value={search}
          onChange={setSearch}
          placeholder={t("search.medicines_placeholder")}
        />
      </div>
      {medicines !== undefined && medicines.length > 1 && !search.trim() && (
        <div style={{ marginBottom: "0.75rem" }}>
          <SortBar value={sortKey} onChange={changeSort} />
        </div>
      )}

      {medicines === undefined && (
        <div style={{ padding: "0 1rem", marginTop: "0.5rem" }}>
          <Skeleton count={4} height={88} />
        </div>
      )}

      {medicines !== undefined && medicines.length === 0 && (
        <EmptyState
          icon="pill"
          title={t("medicines.empty_title")}
          description={t("medicines.empty_description")}
          action={
            <Link to="/medicines/new" style={{ textDecoration: "none" }}>
              <Button variant="primary" size="md" iconLeft={<Icon name="plus" size={18} />}>
                {t("medicines.add_button")}
              </Button>
            </Link>
          }
        />
      )}

      {medicines !== undefined && medicines.length > 0 && filtered.length === 0 && (
        <EmptyState
          icon="search"
          title={t("medicines.no_matches_title")}
          description={t("medicines.no_matches_description", { search })}
        />
      )}

      <div className="grid-cards-lg">
        {filtered.map((m) => (
          <MedicineCard
            key={m.id}
            medicine={m}
            onAdjust={(delta) => handleAdjust(m.id, delta)}
            onDelete={() => handleDelete(m.id, m.name)}
            lowStockThreshold={lowStockThreshold}
            isAmbiguous={ambiguousNames.has(m.name)}
            now={now}
          />
        ))}
      </div>

      {medicines !== undefined && medicines.length > 0 && (
        <FAB to="/medicines/new" label={t("medicines.add_button")}>
          <Icon name="plus" size={28} />
        </FAB>
      )}
    </>
  );
}

// comparator returns the sort function for a given SortKey. Pulled out so
// it's testable and the inline render stays small. Records without an
// expiration_date are pushed to the bottom of that sort.
function comparator(key: SortKey): (a: LocalMedicine, b: LocalMedicine) => number {
  switch (key) {
    case "name":
      return (a, b) => a.name.localeCompare(b.name);
    case "quantity":
      return (a, b) => a.quantity - b.quantity;
    case "expiration":
      return (a, b) => {
        const da = a.expiration_date ?? "";
        const db = b.expiration_date ?? "";
        if (!da && !db) return a.name.localeCompare(b.name);
        if (!da) return 1;
        if (!db) return -1;
        return da.localeCompare(db);
      };
    case "recent":
      return (a, b) => b.updated_at.localeCompare(a.updated_at);
  }
}
