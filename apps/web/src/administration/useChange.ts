import { useEffect, useRef, useState } from "react";
import { AdministrationOutcomeUnknown, administrationMessage } from "../api/administration";
import type { AdministrationIntent, AdministrationPort } from "./Recovery";

export function useChange(port: AdministrationPort) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const running = useRef(false);
  const mounted = useRef(false);
  const request = useRef<AbortController | undefined>(undefined);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current?.abort(); }; }, []);
  async function run<T>(intent: AdministrationIntent, operation: (signal: AbortSignal) => Promise<T>) {
    if (running.current || port.pending[intent.scope]) return;
    running.current = true; setBusy(true); setError("");
    const controller = new AbortController(); request.current = controller;
    const key = crypto.randomUUID();
    port.remember(intent.scope, { ...intent, status: "dispatched" }); port.busy(key, true);
    try {
      const value = await operation(controller.signal);
      port.remember(intent.scope);
      return mounted.current && !controller.signal.aborted ? { ok: true as const, value } : undefined;
    } catch (e) {
      if (e instanceof AdministrationOutcomeUnknown) port.remember(intent.scope, { ...intent, status: "unknown" });
      else port.remember(intent.scope);
      if (mounted.current && !controller.signal.aborted) {
        setError(administrationMessage(e));
        return { ok: false as const };
      }
    } finally {
      port.busy(key, false); running.current = false;
      if (mounted.current) setBusy(false);
    }
  }
  return { busy, error, run, clearError: () => setError("") };
}
