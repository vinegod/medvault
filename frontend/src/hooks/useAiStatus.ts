import { useEffect, useState } from "react";
import { useAuth } from "../contexts/AuthContext";
import { aiStatus } from "../services/ai";
import type { AiStatus } from "../types";

const DISABLED: AiStatus = { enabled: false, source: "none", local: false };

// useAiStatus returns the assistant status, disabled while not connected.
// A new refreshKey or household reloads it in the background, keeping the
// previous value until the reply arrives.
export function useAiStatus(refreshKey: unknown = null): AiStatus {
  const { status, activeHouseholdId } = useAuth();
  const [result, setResult] = useState<{ household: string | null; value: AiStatus } | null>(null);

  useEffect(() => {
    if (status !== "connected") return;
    let cancelled = false;
    aiStatus().then((res) => {
      if (!cancelled) setResult({ household: activeHouseholdId, value: res.data ?? DISABLED });
    });
    return () => {
      cancelled = true;
    };
  }, [status, activeHouseholdId, refreshKey]);

  if (status !== "connected" || !result || result.household !== activeHouseholdId) {
    return DISABLED;
  }
  return result.value;
}
