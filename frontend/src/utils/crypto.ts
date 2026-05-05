// AES-GCM passphrase-encrypted backup helpers, used by Settings export/import
// when the user opts into a passphrase. The format is intentionally
// self-describing so an importer can detect encrypted vs. plain v1 exports
// without out-of-band metadata.
//
// Key derivation: PBKDF2-HMAC-SHA256, 600 000 iterations (OWASP 2024
// recommendation). 16-byte random salt per export, 12-byte random IV per
// encryption. AES-GCM 256.

const PBKDF2_ITERATIONS = 600_000;
const KEY_LENGTH_BITS = 256;

export interface EncryptedExport {
  version: 2;
  encrypted: true;
  iterations: number;
  salt: string; // base64
  iv: string; // base64
  ciphertext: string; // base64
}

export async function encryptJSON(plaintext: string, passphrase: string): Promise<EncryptedExport> {
  const enc = new TextEncoder();
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await deriveKey(passphrase, salt);

  const cipherBuf = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: iv as BufferSource },
    key,
    enc.encode(plaintext) as BufferSource
  );

  return {
    version: 2,
    encrypted: true,
    iterations: PBKDF2_ITERATIONS,
    salt: bytesToBase64(salt),
    iv: bytesToBase64(iv),
    ciphertext: bytesToBase64(new Uint8Array(cipherBuf)),
  };
}

export async function decryptJSON(env: EncryptedExport, passphrase: string): Promise<string> {
  const salt = base64ToBytes(env.salt);
  const iv = base64ToBytes(env.iv);
  const ciphertext = base64ToBytes(env.ciphertext);
  const key = await deriveKey(passphrase, salt, env.iterations);

  // crypto.subtle.decrypt throws on auth-tag mismatch; let the caller turn
  // that into a user-friendly "wrong passphrase" message.
  const plainBuf = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: iv as BufferSource },
    key,
    ciphertext as BufferSource
  );

  return new TextDecoder().decode(plainBuf);
}

async function deriveKey(
  passphrase: string,
  salt: Uint8Array,
  iterations: number = PBKDF2_ITERATIONS
): Promise<CryptoKey> {
  const baseKey = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(passphrase) as BufferSource,
    "PBKDF2",
    false,
    ["deriveKey"]
  );
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", salt: salt as BufferSource, iterations, hash: "SHA-256" },
    baseKey,
    { name: "AES-GCM", length: KEY_LENGTH_BITS },
    false,
    ["encrypt", "decrypt"]
  );
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

function base64ToBytes(b64: string): Uint8Array {
  const binary = atob(b64);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}
