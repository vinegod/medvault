import {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  type ReactNode,
} from "react";
import {
  initServerConnection,
  connectToServer,
  disconnectFromServer,
  onServerStatusChange,
  getServerStatus,
} from "../services/server";
import { getMeta } from "../services/db";
import type { ServerConnectionStatus } from "../types";

// ── Shape of what the context exposes ────────────────────────────────────────

interface User {
  id: string;
  username: string;
}

interface AuthContextValue {
  // Current server connection state
  status: ServerConnectionStatus;
  // Null when not connected to a server
  user: User | null;
  serverUrl: string | null;
  // True once the initial session-restore attempt has finished
  initialized: boolean;

  connect: (
    url: string,
    username: string,
    password: string,
    isRegister: boolean
  ) => Promise<{ success: boolean; error?: string }>;
  disconnect: () => Promise<void>;
}

// ── Context ───────────────────────────────────────────────────────────────────

const AuthContext = createContext<AuthContextValue | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<ServerConnectionStatus>(getServerStatus);
  const [user, setUser] = useState<User | null>(null);
  const [serverUrl, setServerUrl] = useState<string | null>(null);
  const [initialized, setInitialized] = useState(false);

  // Load persisted user info from IndexedDB and restore the server session.
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

      // Try to reconnect using the stored refresh token.
      await initServerConnection();
      setInitialized(true);
    }

    restore();

    // Subscribe to future status changes (e.g. token refresh failures).
    return onServerStatusChange(setStatus);
  }, []);

  const connect = useCallback(
    async (url: string, username: string, password: string, isRegister: boolean) => {
      const result = await connectToServer(url, username, password, isRegister);
      if (result.success) {
        // Re-read the user from IndexedDB — server.ts wrote it there.
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
    setStatus("disconnected");
  }, []);

  return (
    <AuthContext.Provider value={{ status, user, serverUrl, initialized, connect, disconnect }}>
      {children}
    </AuthContext.Provider>
  );
}

// ── Consumer hook ─────────────────────────────────────────────────────────────

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}
