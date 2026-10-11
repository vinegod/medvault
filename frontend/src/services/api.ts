import type { ApiResponse } from "../types";
import { getMeta, setMeta } from "./db";

// Per-request timeout. Long enough to absorb a slow self-hosted SQLite
// commit, short enough that a wedged server doesn't hang the UI forever.
const REQUEST_TIMEOUT_MS = 15_000;

/**
 * API client for communicating with the optional MedVault server.
 * This is ONLY used when a server is connected. In standalone mode,
 * all data operations go directly through db.ts → IndexedDB.
 */
class ApiClient {
  private baseUrl: string = "";
  private accessToken: string | null = null;
  // Single in-flight refresh promise. Two parallel 401 responses share it
  // instead of each issuing their own refresh — rotation would otherwise
  // invalidate the first refresh's token before its retry could use it.
  private refreshing: Promise<boolean> | null = null;

  setBaseUrl(url: string): void {
    this.baseUrl = url;
  }

  setAccessToken(token: string | null): void {
    this.accessToken = token;
  }

  // Read-only accessors used by sibling services (photos.ts) that need to
  // build their own non-JSON requests (multipart upload, raw blob fetch)
  // while still going through the same auth token + base URL.
  getBaseUrl(): string {
    return this.baseUrl;
  }

  getAccessToken(): string | null {
    return this.accessToken;
  }

  // A FormData body is sent as multipart with the browser's own boundary.
  async request<T>(
    method: string,
    path: string,
    body?: unknown,
    timeoutMs: number = REQUEST_TIMEOUT_MS
  ): Promise<ApiResponse<T>> {
    if (!this.baseUrl) {
      return { data: null, error: { code: "NO_SERVER", message: "No server configured" } };
    }

    const isForm = body instanceof FormData;
    const headers: Record<string, string> = isForm ? {} : { "Content-Type": "application/json" };

    if (this.accessToken) {
      headers["Authorization"] = `Bearer ${this.accessToken}`;
    }

    let response: Response;
    try {
      response = await fetch(`${this.baseUrl}${path}`, {
        method,
        headers,
        body: isForm ? body : body ? JSON.stringify(body) : undefined,
        signal: AbortSignal.timeout(timeoutMs),
      });
    } catch (err) {
      // Network failure, timeout, abort — present as a structured envelope
      // so callers don't have to wrap every request in try/catch.
      const message = err instanceof Error ? err.message : String(err);
      return { data: null, error: { code: "NETWORK", message } };
    }

    // Handle 401 — try refresh
    if (response.status === 401 && path !== "/api/auth/refresh") {
      const refreshed = await this.tryRefresh();
      if (refreshed) {
        return this.request<T>(method, path, body, timeoutMs);
      }
      // Refresh failed — token expired, user needs to reconnect
      return {
        data: null,
        error: { code: "AUTH_EXPIRED", message: "Session expired. Please reconnect to server." },
      };
    }

    // Defensive parse: a misconfigured upstream (nginx default page,
    // captive portal, gateway HTML) would otherwise throw inside json()
    // and propagate, breaking every caller's "always returns envelope"
    // assumption. Synthesize a NETWORK envelope instead.
    try {
      return (await response.json()) as ApiResponse<T>;
    } catch {
      return {
        data: null,
        error: { code: "NETWORK", message: `unexpected response (status ${response.status})` },
      };
    }
  }

  async get<T>(path: string): Promise<ApiResponse<T>> {
    return this.request<T>("GET", path);
  }

  async post<T>(path: string, body: unknown): Promise<ApiResponse<T>> {
    return this.request<T>("POST", path, body);
  }

  async put<T>(path: string, body: unknown): Promise<ApiResponse<T>> {
    return this.request<T>("PUT", path, body);
  }

  async patch<T>(path: string, body: unknown): Promise<ApiResponse<T>> {
    return this.request<T>("PATCH", path, body);
  }

  async delete<T>(path: string): Promise<ApiResponse<T>> {
    return this.request<T>("DELETE", path);
  }

  private tryRefresh(): Promise<boolean> {
    // Reuse any refresh already in flight so concurrent 401s don't each
    // burn their own refresh token (rotation would invalidate them).
    if (!this.refreshing) {
      this.refreshing = this.doRefresh().finally(() => {
        this.refreshing = null;
      });
    }
    return this.refreshing;
  }

  private async doRefresh(): Promise<boolean> {
    try {
      const refreshToken = await getMeta("refresh_token");
      if (!refreshToken) return false;

      const response = await fetch(`${this.baseUrl}/api/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: refreshToken }),
        signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
      });

      if (!response.ok) return false;

      const data = await response.json();
      if (data.data?.access_token) {
        this.accessToken = data.data.access_token;
        if (data.data.refresh_token) {
          await setMeta("refresh_token", data.data.refresh_token);
        }
        return true;
      }
      return false;
    } catch {
      return false;
    }
  }
}

export const apiClient = new ApiClient();
