import { useState, useEffect, useId, useRef, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  addMedicine,
  updateMedicine,
  getMedicineById,
  getDistinctCategories,
} from "../services/db";
import { Button } from "../components/Button";
import { Icon } from "../components/Icon";
import { useToast } from "../contexts/ToastContext";
import { NameSuggest, joinDrugNotes } from "../components/NameSuggest";
import { resizeImage, uploadPhoto, fetchPhoto, deletePhoto } from "../services/photos";
import { defaultDaysAfterOpening } from "../utils/effectiveExpiration";
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
  opened_at: "",
  days_after_opening: null,
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
  const [categorySuggestions, setCategorySuggestions] = useState<string[]>([]);
  const categoryListId = useId();

  // Photo state. photoHash is the hash currently saved on the medicine
  // (drives the existing-thumbnail render); photoBusy gates the picker
  // during upload. photoCache holds the most-recently-fetched (hash, url)
  // pair so we can derive `photoUrl` instead of clearing it via setState
  // when photoHash transitions to null (which would be a cascading render).
  const [photoHash, setPhotoHash] = useState<string | null>(null);
  const [photoCache, setPhotoCache] = useState<{ hash: string; url: string } | null>(null);
  const [photoBusy, setPhotoBusy] = useState(false);
  const photoInputRef = useRef<HTMLInputElement | null>(null);
  const photoUrl = photoCache && photoCache.hash === photoHash ? photoCache.url : null;

  useEffect(() => {
    getDistinctCategories().then(setCategorySuggestions);
  }, []);

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
          opened_at: m.opened_at ?? "",
          days_after_opening: m.days_after_opening,
          storage_location: m.storage_location,
          barcode: m.barcode,
          notes: m.notes,
        });
        setPhotoHash(m.photo_hash);
      }
    });
  }, [id]);

  // Resolve the photo to a Blob URL whenever a hash is present. Cleanup
  // revokes the URL on hash change / unmount. When photoHash transitions
  // to null we *don't* call setState here — the derived `photoUrl` above
  // handles it by failing the cache-key match.
  useEffect(() => {
    if (!id || !photoHash) return;
    let url: string | null = null;
    let cancelled = false;
    fetchPhoto(id, photoHash).then((u) => {
      if (cancelled) {
        if (u) URL.revokeObjectURL(u);
        return;
      }
      if (u) {
        url = u;
        setPhotoCache({ hash: photoHash, url: u });
      }
    });
    return () => {
      cancelled = true;
      if (url) URL.revokeObjectURL(url);
    };
  }, [id, photoHash]);

  function set<K extends keyof MedicineInput>(key: K, value: MedicineInput[K]) {
    setFields((f) => ({ ...f, [key]: value }));
  }

  // changeUnit is `set("unit", ...)` plus the days-after-opening default
  // sync. Done at the call site (rather than via a unit-watching effect)
  // so the lint rule against setState-in-effect stays clean. Existing
  // custom values are preserved so editing the unit doesn't silently
  // overwrite a deliberate "30 days" the user typed earlier.
  function changeUnit(unit: MedicineUnit) {
    setFields((f) => {
      const next = { ...f, unit };
      if (f.days_after_opening == null) {
        const def = defaultDaysAfterOpening(unit);
        if (def != null) next.days_after_opening = def;
      }
      return next;
    });
  }

  async function handlePhotoFile(file: File) {
    if (!id) {
      // No medicine id yet (still on Create) — instruct the user. We could
      // queue the upload until after save, but the extra state machine
      // isn't worth it; saving first is a reasonable expectation.
      setError(t("medicine_form.save_before_photo"));
      return;
    }
    setPhotoBusy(true);
    try {
      const { blob, hash } = await resizeImage(file);
      const res = await uploadPhoto(id, blob, hash);
      if (!res.ok) {
        toast.error(res.error ?? t("medicine_form.photo_failed"));
        return;
      }
      setPhotoHash(hash);
      toast.success(t("medicine_form.photo_uploaded"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("medicine_form.photo_failed"));
    } finally {
      setPhotoBusy(false);
    }
  }

  async function handlePhotoRemove() {
    if (!id || !photoHash) return;
    setPhotoBusy(true);
    const ok = await deletePhoto(id, photoHash);
    setPhotoBusy(false);
    if (ok) {
      // photoUrl is derived from (photoCache, photoHash); clearing the
      // hash makes the cache-key match fail on the next render.
      setPhotoHash(null);
      toast.success(t("medicine_form.photo_removed"));
    } else {
      toast.error(t("medicine_form.photo_failed"));
    }
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
      opened_at: fields.opened_at || (null as unknown as string),
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
        <NameSuggest
          value={fields.name}
          onChange={(v) => set("name", v)}
          onPick={(s) =>
            setFields((f) => ({
              ...f,
              name: s.name,
              category: f.category === "" ? (s.ingredients ?? "") : f.category,
              notes: f.notes === "" ? joinDrugNotes(s.inn, s.pharma_group) : f.notes,
            }))
          }
          placeholder={t("medicine_form.name_placeholder")}
          maxLength={200}
          required
          autoFocus={!isEdit}
          inputStyle={inp}
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
            onChange={(e) => {
              const n = Number(e.target.value);
              set("quantity", Number.isNaN(n) ? 0 : Math.max(0, n));
            }}
          />
        </Field>
        <Field label={t("medicine_form.unit")} style={{ flex: 1 }}>
          <select
            style={inp}
            value={fields.unit}
            onChange={(e) => changeUnit(e.target.value as MedicineUnit)}
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
          list={categoryListId}
          autoComplete="off"
        />
        <datalist id={categoryListId}>
          {categorySuggestions.map((c) => (
            <option key={c} value={c} />
          ))}
        </datalist>
      </Field>

      <Field label={t("medicine_form.expiration_date")}>
        <input
          style={inp}
          type="date"
          value={fields.expiration_date ?? ""}
          onChange={(e) => set("expiration_date", e.target.value)}
        />
      </Field>

      <div style={{ display: "flex", gap: "0.75rem" }}>
        <Field label={t("medicine_form.opened_at")} style={{ flex: 1 }}>
          <input
            style={inp}
            type="date"
            value={fields.opened_at ?? ""}
            onChange={(e) => set("opened_at", e.target.value)}
          />
        </Field>
        <Field label={t("medicine_form.days_after_opening")} style={{ flex: 1 }}>
          <input
            style={inp}
            type="number"
            inputMode="numeric"
            min={1}
            max={3650}
            value={fields.days_after_opening ?? ""}
            onChange={(e) => {
              const raw = e.target.value;
              if (raw === "") {
                set("days_after_opening", null);
                return;
              }
              const n = Number(raw);
              set("days_after_opening", Number.isNaN(n) ? null : n);
            }}
            placeholder={t("medicine_form.days_after_opening_placeholder")}
          />
        </Field>
      </div>

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
        <small
          style={{
            color: fields.notes.length > 480 ? "var(--color-error)" : "var(--color-text-muted)",
            fontSize: "0.78rem",
          }}
        >
          {t("medicine_form.notes_counter", { count: fields.notes.length })}
        </small>
      </Field>

      {/* Photo: only available on existing medicines (needs an id for the
          upload endpoint). Hidden on create until the user saves once. */}
      {isEdit && (
        <Field label={t("medicine_form.photo")}>
          <input
            ref={photoInputRef}
            type="file"
            accept="image/*"
            capture="environment"
            style={{ display: "none" }}
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) handlePhotoFile(file);
              e.target.value = "";
            }}
          />
          {photoUrl && (
            <img
              src={photoUrl}
              alt={t("medicine_form.photo_alt")}
              style={{
                width: "100%",
                maxHeight: 240,
                objectFit: "contain",
                background: "var(--color-bg)",
                borderRadius: "var(--radius)",
                border: "1px solid var(--color-border)",
                marginBottom: "0.5rem",
              }}
            />
          )}
          <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => photoInputRef.current?.click()}
              disabled={photoBusy}
              iconLeft={<Icon name="upload" size={14} />}
            >
              {photoHash ? t("medicine_form.photo_replace") : t("medicine_form.photo_attach")}
            </Button>
            {photoHash && (
              <Button
                type="button"
                variant="danger"
                size="sm"
                onClick={handlePhotoRemove}
                disabled={photoBusy}
                iconLeft={<Icon name="trash" size={14} />}
              >
                {t("medicine_form.photo_remove")}
              </Button>
            )}
          </div>
        </Field>
      )}

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
        {required && <span style={{ color: "var(--color-primary)" }}> *</span>}
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
