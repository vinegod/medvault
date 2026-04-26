import { useState, useMemo } from "react";
import { useLiveQuery } from "dexie-react-hooks";
import { useTranslation } from "react-i18next";
import Fuse from "fuse.js";
import { db, deleteMedicine, adjustQuantity } from "../services/db";
import { MedicineCard } from "../components/MedicineCard";
import { SearchBar } from "../components/SearchBar";
import { EmptyState } from "../components/EmptyState";
import { FAB } from "../components/FAB";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { useToast } from "../contexts/ToastContext";
import { Link } from "react-router-dom";

export default function Medicines() {
  const [search, setSearch] = useState("");
  const toast = useToast();
  const { t } = useTranslation();

  const medicines = useLiveQuery(
    () => db.medicines.filter((m) => m.deleted_at === null).sortBy("name"),
    []
  );

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

  const filtered = useMemo(() => {
    if (!medicines) return [];
    if (!search.trim()) return medicines;
    return fuse!.search(search).map((r) => r.item);
  }, [fuse, medicines, search]);

  async function handleAdjust(id: string, delta: number) {
    const newQty = await adjustQuantity(id, delta);
    if (delta < 0 && newQty === 0) {
      toast.info(t("medicines.out_of_stock"));
    }
  }

  async function handleDelete(id: string, name: string) {
    await deleteMedicine(id);
    toast.success(t("medicines.deleted", { name }));
  }

  return (
    <>
      <div style={{ marginBottom: "1rem" }}>
        <SearchBar value={search} onChange={setSearch} placeholder={t("search.medicines_placeholder")} />
      </div>

      {medicines === undefined && (
        <p style={{ color: "var(--color-text-muted)", textAlign: "center", marginTop: "2rem" }}>
          {t("medicines.loading")}
        </p>
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

      <div style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
        {filtered.map((m) => (
          <MedicineCard
            key={m.id}
            medicine={m}
            onAdjust={(delta) => handleAdjust(m.id, delta)}
            onDelete={() => handleDelete(m.id, m.name)}
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
