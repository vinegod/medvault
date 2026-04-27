// Core medicine record — used in both IndexedDB and server
export interface Medicine {
  id: string;
  name: string;
  quantity: number;
  unit: MedicineUnit;
  category: string;
  expiration_date: string | null; // ISO date string "2027-03-15"
  storage_location: string;
  barcode: string;
  notes: string;
  created_at: string; // ISO datetime
  updated_at: string; // ISO datetime
  deleted_at: string | null; // ISO datetime, null = active
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
  expiration_date: string;
  storage_location: string;
  barcode: string;
  notes: string;
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

// One row of GET /api/households — used by the switcher and Household page.
export interface MembershipSummary {
  id: string;
  name: string;
  role: "owner" | "member";
  joined_at: string;
  is_active: boolean;
}

// Export/Import format (for manual JSON backup)
export interface MedVaultExport {
  version: 1;
  exported_at: string;
  medicines: Medicine[];
  settings: Record<string, string>;
}
