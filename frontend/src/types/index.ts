// Core medicine record — used in both IndexedDB and server
//
// opened_at + days_after_opening together produce an "effective" expiration
// (see utils/effectiveExpiration.ts). Both are optional. photo_hash is the
// SHA-256 hex of the most recent uploaded photo, served by
// /api/medicines/{id}/photo. Photos themselves are cached in IndexedDB.
// disposed_at is set only on deleted records that were thrown away.
export interface Medicine {
  id: string;
  name: string;
  quantity: number;
  unit: MedicineUnit;
  category: string;
  active_ingredient: string | null;
  expiration_date: string | null; // ISO date string "2027-03-15"
  opened_at: string | null;
  days_after_opening: number | null;
  storage_location: string;
  barcode: string;
  notes: string;
  photo_hash: string | null;
  created_at: string; // ISO datetime
  updated_at: string; // ISO datetime
  deleted_at: string | null; // ISO datetime, null = active
  disposed_at: string | null; // ISO datetime
}

// Client-side record with sync tracking (stored in IndexedDB)
export interface LocalMedicine extends Medicine {
  sync_status: SyncStatus;
}

// "local" = standalone mode, never synced
// "synced" = matches server version
// "pending" = modified locally, not yet synced to server
// "conflict" = (future) flagged for manual resolution
export type SyncStatus = "local" | "synced" | "pending" | "conflict";

export type MedicineUnit =
  | "tablets"
  | "capsules"
  | "ml"
  | "mg"
  | "g"
  | "pieces"
  | "drops"
  | "sachets"
  | "patches";

// Form input (what the user fills in)
export interface MedicineInput {
  name: string;
  quantity: number;
  unit: MedicineUnit;
  category: string;
  active_ingredient: string;
  expiration_date: string;
  opened_at: string;
  days_after_opening: number | null;
  storage_location: string;
  barcode: string;
  notes: string;
}

// One row of the household activity log.
export interface AuditEntry {
  id: string;
  username: string;
  action:
    | "medicine.create"
    | "medicine.update"
    | "medicine.delete"
    | "medicine.restore"
    | "medicine.dispose"
    | "medicine.quantity"
    | "household.rename"
    | "household.member.add"
    | "household.member.remove"
    | "household.transfer";
  target_type: string;
  target_id: string | null;
  target_name: string | null;
  quantity_before: number | null;
  quantity_after: number | null;
  at: string;
}

// Sync request/response (only used when server is connected)
export interface SyncRequest {
  last_sync: string | null;
  changes: Medicine[];
}

export interface SyncResponse {
  records: Medicine[];
  server_time: string;
}

// API envelope (server communication only)
export interface ApiResponse<T> {
  data: T | null;
  error: ApiError | null;
}

export interface ApiError {
  code: string;
  message: string;
}

// Auth (server mode only)
export interface AuthTokens {
  access_token: string;
  refresh_token: string;
}

export interface LoginRequest {
  username: string;
  password: string;
}

export interface AuthResponse {
  user: {
    id: string;
    username: string;
    created_at: string;
  };
  access_token: string;
  refresh_token: string;
}

// Expiration status helpers
export type ExpirationStatus = "expired" | "expiring_soon" | "ok" | "no_date";

export interface ExpirationSummary {
  expired: number;
  expiring_within_30_days: number;
  expiring_within_90_days: number;
}

// Server connection state
export type ServerConnectionStatus = "disconnected" | "connecting" | "connected" | "error";

export interface ServerConfig {
  url: string;
  status: ServerConnectionStatus;
  last_sync: string | null;
  user: {
    id: string;
    username: string;
  } | null;
}

// Household (server mode only)
export interface MemberView {
  user_id: string;
  username: string;
  role: "owner" | "member";
  joined_at: string;
}

export interface Household {
  id: string;
  name: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface HouseholdDetail extends Household {
  members: MemberView[];
}

export interface Invite {
  code: string;
  household_id: string;
  invited_by: string;
  expires_at: string;
  created_at?: string;
}

// One row of GET /api/drugs/search. Pointer fields on the server map to
// undefined here when the source DB had NULL.
export interface DrugSuggestion {
  name: string;
  inn?: string;
  ingredients?: string;
  pharma_group?: string;
  country: string;
}

// One row of GET /api/households — used by the switcher and Household page.
export interface MembershipSummary {
  id: string;
  name: string;
  role: "owner" | "member";
  joined_at: string;
  is_active: boolean;
}

export type AiProvider = "openai" | "anthropic";
export type AiSource = "web" | "env" | "none";

export interface AiStatus {
  enabled: boolean;
  source: AiSource;
  local: boolean;
}

// GET /api/ai/settings. The API key is never returned; base_url only to admins.
export interface AiSettings {
  provider: AiProvider;
  base_url: string;
  model: string;
  vision_model: string;
  api_key_set: boolean;
  source: AiSource;
  can_edit: boolean;
}

// PUT /api/ai/settings. Omit api_key to keep the stored key (not allowed when
// provider or base_url changes), "" clears it.
export interface AiSettingsInput {
  provider: AiProvider;
  base_url: string;
  model: string;
  vision_model: string;
  api_key?: string;
}

export interface AiScanDraft {
  name: string;
  quantity: number;
  unit: MedicineUnit;
  active_ingredient: string | null;
  expiration_date: string | null;
  days_after_opening: number | null;
  barcode: string;
}

export interface AiMatch {
  id: string;
  name: string;
  reason: "barcode" | "name" | "ingredient";
}

export interface AiScanResult {
  draft: AiScanDraft;
  matches: AiMatch[];
}

export interface ChatMessage {
  role: "user" | "assistant";
  content: string;
}

export interface AiReply {
  text: string;
  warning?: string;
  medicine_ids: string[];
}

// Export/Import format (for manual JSON backup)
export interface MedVaultExport {
  version: 1;
  exported_at: string;
  medicines: Medicine[];
  settings: Record<string, string>;
}
