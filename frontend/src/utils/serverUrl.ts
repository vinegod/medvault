/**
 * Returns null when the URL is valid (http/https, non-empty host),
 * or a translation key string when it is not.
 */
export function validateServerUrl(s: string): string | null {
  if (!s.trim()) return "auth.invalid_url";
  try {
    const u = new URL(s);
    if (u.protocol !== "http:" && u.protocol !== "https:") return "auth.invalid_url";
    if (!u.hostname) return "auth.invalid_url";
    return null;
  } catch {
    return "auth.invalid_url";
  }
}
