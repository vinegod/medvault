import { useState, useEffect, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { addMedicine, updateMedicine, getMedicineById } from "../services/db";
import { Button } from "../components/Button";
import { useToast } from "../contexts/ToastContext";
import type { MedicineInput, MedicineUnit } from "../types";

const UNITS: MedicineUnit[] = [
  "tablets",
  "capsules",
  "ml",
  "mg",
  "g",
  "pieces",
  "drops",
  "sachets",
  "patches",
];

const EMPTY: MedicineInput = {
  name: "",
  quantity: 0,
  unit: "tablets",
  category: "",
  expiration_date: "",
  storage_location: "",
  barcode: "",
  notes: "",
};

export default function MedicineForm() {
  const { id } = useParams<{ id?: string }>();
  const navigate = useNavigate();
  const toast = useToast();
  const { t } = useTranslation();
  const isEdit = Boolean(id);

  const [fields, setFields] = useState<MedicineInput>(EMPTY);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!id) return;
    getMedicineById(id).then((m) => {
      if (m) {
        setFields({
          name: m.name,
          quantity: m.quantity,
          unit: m.unit,
          category: m.category,
          expiration_date: m.expiration_date ?? "",
          storage_location: m.storage_location,
          barcode: m.barcode,
          notes: m.notes,
        });
      }
    });
  }, [id]);

  function set<K extends keyof MedicineInput>(key: K, value: MedicineInput[K]) {
    setFields((f) => ({ ...f, [key]: value }));
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");

    if (!fields.name.trim()) {
      setError(t("medicine_form.name_required"));
      return;
    }
    if (fields.quantity < 0) {
      setError(t("medicine_form.quantity_negative"));
      return;
    }

    const payload: MedicineInput = {
      ...fields,
      expiration_date: fields.expiration_date || (null as unknown as string),
    };

    setLoading(true);
    try {
      if (isEdit && id) {
        await updateMedicine(id, payload);
        toast.success(t("medicine_form.updated", { name: fields.name }));
      } else {
        await addMedicine(payload);
        toast.success(t("medicine_form.added", { name: fields.name }));
      }
      navigate("/medicines");
    } catch {
      setError(t("medicine_form.save_failed"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} style={formStyle}>
      <Field label={t("medicine_form.name")} required>
        <input
          style={inp}
          value={fields.name}
          onChange={(e) => set("name", e.target.value)}
          placeholder={t("medicine_form.name_placeholder")}
          maxLength={200}
          required
          autoFocus={!isEdit}
        />
      </Field>

      <div style={{ display: "flex", gap: "0.75rem" }}>
        <Field label={t("medicine_form.quantity")} style={{ flex: 1 }}>
          <input
            style={inp}
            type="number"
            inputMode="numeric"
            min={0}
            max={99999}
            value={fields.quantity}
            onChange={(e) => set("quantity", Number(e.target.value))}
          />
        </Field>
        <Field label={t("medicine_form.unit")} style={{ flex: 1 }}>
          <select
            style={inp}
            value={fields.unit}
            onChange={(e) => set("unit", e.target.value as MedicineUnit)}
          >
            {UNITS.map((u) => (
              <option key={u} value={u}>
                {t(`units.${u}`)}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <Field label={t("medicine_form.category")}>
        <input
          style={inp}
          value={fields.category}
          onChange={(e) => set("category", e.target.value)}
          placeholder={t("medicine_form.category_placeholder")}
          maxLength={50}
        />
      </Field>

      <Field label={t("medicine_form.expiration_date")}>
        <input
          style={inp}
          type="date"
          value={fields.expiration_date ?? ""}
          onChange={(e) => set("expiration_date", e.target.value)}
        />
      </Field>

      <Field label={t("medicine_form.storage_location")}>
        <input
          style={inp}
          value={fields.storage_location}
          onChange={(e) => set("storage_location", e.target.value)}
          placeholder={t("medicine_form.storage_placeholder")}
          maxLength={100}
        />
      </Field>

      <Field label={t("medicine_form.barcode")}>
        <input
          style={inp}
          value={fields.barcode}
          onChange={(e) => set("barcode", e.target.value)}
          placeholder={t("medicine_form.barcode_placeholder")}
          maxLength={50}
        />
      </Field>

      <Field label={t("medicine_form.notes")}>
        <textarea
          style={{ ...inp, resize: "vertical", minHeight: 80 }}
          value={fields.notes}
          onChange={(e) => set("notes", e.target.value)}
          placeholder={t("medicine_form.notes_placeholder")}
          maxLength={500}
        />
      </Field>

      {error && <p style={{ color: "var(--color-error)", fontSize: "0.9rem" }}>{error}</p>}

      <div style={{ display: "flex", gap: "0.75rem", marginTop: "0.5rem" }}>
        <Button type="button" variant="secondary" onClick={() => navigate(-1)} fullWidth>
          {t("common.cancel")}
        </Button>
        <Button type="submit" variant="primary" disabled={loading} fullWidth>
          {loading ? t("medicine_form.saving") : isEdit ? t("common.save") : t("common.add")}
        </Button>
      </div>
    </form>
  );
}

function Field({
  label,
  required,
  children,
  style,
}: {
  label: string;
  required?: boolean;
  children: React.ReactNode;
  style?: React.CSSProperties;
}) {
  return (
    <label style={{ ...lblStyle, ...style }}>
      <span style={{ fontSize: "0.85rem", color: "var(--color-text-muted)" }}>
        {label}
        {required && <span style={{ color: "var(--color-accent)" }}> *</span>}
      </span>
      {children}
    </label>
  );
}

const formStyle: React.CSSProperties = { display: "flex", flexDirection: "column", gap: "1rem" };
const lblStyle: React.CSSProperties = { display: "flex", flexDirection: "column", gap: "0.35rem" };
const inp: React.CSSProperties = {
  background: "var(--color-surface)",
  border: "1px solid var(--color-border)",
  borderRadius: "var(--radius)",
  padding: "0.7rem 0.85rem",
  color: "var(--color-text)",
  fontSize: "1rem",
  outline: "none",
};
