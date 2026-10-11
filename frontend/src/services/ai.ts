// Client for the /api/ai endpoints.

import { apiClient } from "./api";
import type {
  AiReply,
  AiScanResult,
  AiSettings,
  AiSettingsInput,
  AiStatus,
  ApiResponse,
  ChatMessage,
} from "../types";

const AI_TIMEOUT_MS = 90_000;

export function aiStatus(): Promise<ApiResponse<AiStatus>> {
  return apiClient.get<AiStatus>("/api/ai/status");
}

export function aiSettings(): Promise<ApiResponse<AiSettings>> {
  return apiClient.get<AiSettings>("/api/ai/settings");
}

export function saveAiSettings(input: AiSettingsInput): Promise<ApiResponse<AiSettings>> {
  return apiClient.put<AiSettings>("/api/ai/settings", input);
}

export function resetAiSettings(): Promise<ApiResponse<AiSettings>> {
  return apiClient.delete<AiSettings>("/api/ai/settings");
}

export function aiScan(photo: Blob): Promise<ApiResponse<AiScanResult>> {
  const form = new FormData();
  form.append("photo", photo, "scan.jpg");
  return apiClient.request<AiScanResult>("POST", "/api/ai/scan", form, AI_TIMEOUT_MS);
}

export function aiChat(messages: ChatMessage[]): Promise<ApiResponse<AiReply>> {
  return apiClient.request<AiReply>("POST", "/api/ai/chat", { messages }, AI_TIMEOUT_MS);
}
