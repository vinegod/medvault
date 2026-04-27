import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from "react";
import {
  initServerConnection,
  connectToServer,
  disconnectFromServer,
  onServerStatusChange,
  getServerStatus,
} from "../services/server";
import { apiClient } from "../services/api";
import { rebuildLocalCacheFromServer } from "../services/sync";
import { getMeta } from "../services/db";
import type { ServerConnectionStatus, MembershipSummary } from "../types";

interface User {
  id: string;
  username: string;
}

interface AuthContextValue {
  status: ServerConnectionStatus;
  user: User | null;
  serverUrl: string | null;
  initialized: boolean;

  // Households the user belongs to. Empty array when disconnected or
  // before the first fetch completes.
  households: MembershipSummary[];
  activeHouseholdId: string | null;

  connect: (
    url: string,
    username: string,
    password: string,
    isRegister: boolean
  ) => Promise<{ success: boolean; error?: string }>;
  disconnect: () => Promise<void>;

  // Refetch the household list. Called after rename/leave/create/join so
  // the switcher and Household page reflect the new state.
  refreshHouseholds: () => Promise<void>;
  // Switch active household: calls /switch, then wipes the local cache
  // and re-syncs from the new household. Returns success.
  switchHousehold: (householdId: string) => Promise<boolean>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<ServerConnectionStatus>(getServerStatus);
  const [user, setUser] = useState<User | null>(null);
  const [serverUrl, setServerUrl] = useState<string | null>(null);
  const [initialized, setInitialized] = useState(false);
  const [households, setHouseholds] = useState<MembershipSummary[]>([]);

  const refreshHouseholds = useCallback(async () => {
    const res = await apiClient.get<MembershipSummary[]>("/api/households");
    if (res.data) setHouseholds(res.data);
  }, []);

  // Restore persisted session and listen for status changes.
  useEffect(() => {
    async function restore() {
      const [storedUrl, storedId, storedUsername] = await Promise.all([
        getMeta("server_url"),
        getMeta("user_id"),
        getMeta("username"),
      ]);

      setServerUrl(storedUrl);
      if (storedId && storedUsername) {
        setUser({ id: storedId, username: storedUsername });
      }

      await initServerConnection();
      setInitialized(true);
    }

    restore();
    return onServerStatusChange(setStatus);
  }, []);

  // React to server-status changes: load the household list on connect,
  // clear it on disconnect. The fetch updates state asynchronously after
  // the network call, so the cascading-render lint warning doesn't apply.
  useEffect(() => {
    if (status === "connected") {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      refreshHouseholds();
    } else {
      setHouseholds([]);
    }
  }, [status, refreshHouseholds]);

  const connect = useCallback(
    async (url: string, username: string, password: string, isRegister: boolean) => {
      const result = await connectToServer(url, username, password, isRegister);
      if (result.success) {
        const [id, name, sUrl] = await Promise.all([
          getMeta("user_id"),
          getMeta("username"),
          getMeta("server_url"),
        ]);
        if (id && name) setUser({ id, username: name });
        setServerUrl(sUrl);
        setStatus("connected");
      }
      return result;
    },
    []
  );

  const disconnect = useCallback(async () => {
    await disconnectFromServer();
    setUser(null);
    setServerUrl(null);
    setHouseholds([]);
    setStatus("disconnected");
  }, []);

  const switchHousehold = useCallback(
    async (householdId: string): Promise<boolean> => {
      const res = await apiClient.post("/api/households/switch", {
        household_id: householdId,
      });
      if (res.error) return false;
      await rebuildLocalCacheFromServer();
      await refreshHouseholds();
      return true;
    },
    [refreshHouseholds]
  );

  const activeHouseholdId = households.find((h) => h.is_active)?.id ?? null;

  return (
    <AuthContext.Provider
      value={{
        status,
        user,
        serverUrl,
        initialized,
        households,
        activeHouseholdId,
        connect,
        disconnect,
        refreshHouseholds,
        switchHousehold,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
