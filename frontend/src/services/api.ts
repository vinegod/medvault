import type { ApiResponse } from "../types";
import { getMeta, setMeta } from "./db";

/**
 * API client for communicating with the optional MedVault server.
 * This is ONLY used when a server is connected. In standalone mode,
 * all data operations go directly through db.ts → IndexedDB.
 */
class ApiClient {
  private baseUrl: string = "";
  private accessToken: string | null = null;

  setBaseUrl(url: string): void {
    this.baseUrl = url;
  }

  setAccessToken(token: string | null): void {
    this.accessToken = token;
  }

  async request<T>(method: string, path: string, body?: unknown): Promise<ApiResponse<T>> {
    if (!this.baseUrl) {
      return { data: null, error: { code: "NO_SERVER", message: "No server configured" } };
    }

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };

    if (this.accessToken) {
      headers["Authorization"] = `Bearer ${this.accessToken}`;
    }

    const response = await fetch(`${this.baseUrl}${path}`, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });

    // Handle 401 — try refresh
    if (response.status === 401 && path !== "/api/auth/refresh") {
      const refreshed = await this.tryRefresh();
      if (refreshed) {
        return this.request<T>(method, path, body);
      }
      // Refresh failed — token expired, user needs to reconnect
      return {
        data: null,
        error: { code: "AUTH_EXPIRED", message: "Session expired. Please reconnect to server." },
      };
    }

    return response.json();
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

  private async tryRefresh(): Promise<boolean> {
    try {
      const refreshToken = await getMeta("refresh_token");
      if (!refreshToken) return false;

      const response = await fetch(`${this.baseUrl}/api/auth/refresh`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: refreshToken }),
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
