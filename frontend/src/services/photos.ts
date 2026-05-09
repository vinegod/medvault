// Photo upload / fetch / cache flow. Photos travel out-of-band of the
// sync envelope: medicine rows carry only `photo_hash`, the actual blob
// is uploaded to /api/medicines/{id}/photo and fetched the same way.
// Frontend caches blobs by hash in IndexedDB so offline viewing works
// once the photo has been seen at least once.

import { apiClient } from "./api";

const MAX_DIMENSION = 1024; // resize before upload to keep payload small
const JPEG_QUALITY = 0.85;

// Sub-DB for photo blobs. Keyed by hash (SHA-256 hex computed locally
// after resize so the cache key matches the server's content-address).
// Adding a new Dexie store table requires a versioned schema bump in db.ts;
// for now we piggy-back on the existing `meta` table by prefixing keys.
//
// On second thought — meta.value is `string`, not Blob. We need a real
// blob store. Use a separate Dexie database keyed by hash to avoid touching
// the existing v1 schema.

import Dexie, { type Table } from "dexie";

interface PhotoRow {
  hash: string;
  blob: Blob;
}

class PhotoDB extends Dexie {
  photos!: Table<PhotoRow>;
  constructor() {
    super("MedVaultPhotos");
    this.version(1).stores({ photos: "hash" });
  }
}

const photoDB = new PhotoDB();

// resizeImage: decode -> downscale -> re-encode JPEG. Drops EXIF/orientation
// because the canvas API discards it; for medicine photos that's desired
// (no GPS leaks). Returns the resized blob and its SHA-256 hex.
export async function resizeImage(file: File): Promise<{ blob: Blob; hash: string }> {
  const bitmap = await createImageBitmap(file);
  const ratio = Math.min(1, MAX_DIMENSION / Math.max(bitmap.width, bitmap.height));
  const w = Math.round(bitmap.width * ratio);
  const h = Math.round(bitmap.height * ratio);

  const canvas = document.createElement("canvas");
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("canvas not supported");
  ctx.drawImage(bitmap, 0, 0, w, h);
  bitmap.close();

  const blob: Blob = await new Promise((resolve, reject) => {
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error("encode failed"))),
      "image/jpeg",
      JPEG_QUALITY
    );
  });

  const hash = await sha256Hex(await blob.arrayBuffer());
  return { blob, hash };
}

// uploadPhoto sends the blob to the server. Caches the blob locally on
// success so the next render finds it without a round-trip.
export async function uploadPhoto(
  medicineId: string,
  blob: Blob,
  hash: string
): Promise<{ ok: boolean; error?: string }> {
  const form = new FormData();
  form.append("photo", blob, `${hash}.jpg`);

  // We use fetch directly here rather than apiClient because apiClient
  // forces JSON Content-Type; multipart needs the browser to set its own
  // boundary header. Auth header is replicated manually.
  const baseUrl = apiClient.getBaseUrl();
  const token = apiClient.getAccessToken();
  if (!baseUrl) return { ok: false, error: "No server configured" };

  let res: Response;
  try {
    res = await fetch(`${baseUrl}/api/medicines/${medicineId}/photo`, {
      method: "POST",
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: AbortSignal.timeout(30_000),
    });
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : String(err) };
  }
  if (!res.ok) {
    return { ok: false, error: `upload failed (${res.status})` };
  }
  await photoDB.photos.put({ hash, blob });
  return { ok: true };
}

// fetchPhoto returns a Blob URL for the medicine's photo. Looks in the
// local cache first (offline-capable), falls back to a network fetch and
// populates the cache. Caller is responsible for URL.revokeObjectURL().
export async function fetchPhoto(medicineId: string, hash: string): Promise<string | null> {
  const cached = await photoDB.photos.get(hash);
  if (cached) {
    return URL.createObjectURL(cached.blob);
  }

  const baseUrl = apiClient.getBaseUrl();
  const token = apiClient.getAccessToken();
  if (!baseUrl) return null;

  try {
    const res = await fetch(`${baseUrl}/api/medicines/${medicineId}/photo`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      signal: AbortSignal.timeout(15_000),
    });
    if (!res.ok) return null;
    const blob = await res.blob();
    await photoDB.photos.put({ hash, blob });
    return URL.createObjectURL(blob);
  } catch {
    return null;
  }
}

// deletePhoto clears the row's photo_hash on the server and evicts the
// blob from local cache.
export async function deletePhoto(medicineId: string, hash: string | null): Promise<boolean> {
  const res = await apiClient.delete(`/api/medicines/${medicineId}/photo`);
  if (res.error) return false;
  if (hash) await photoDB.photos.delete(hash);
  return true;
}

async function sha256Hex(buf: ArrayBuffer): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", buf);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
