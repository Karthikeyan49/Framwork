import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createAPIClient, type Session } from "../api/client";
import { config } from "../config";
import { loginClient, type LoginOptions, type LoginReturn } from "./client";

export interface Connection { token: string; session: Session }
interface Authentication {
  connection?: Connection;
  connect: (value: Connection) => void;
  disconnect: (expected?: Connection) => void;
  options?: LoginOptions;
  optionsError: boolean;
  reloadOptions: () => void;
  callbackWork: () => Promise<Connection> | undefined;
  acceptCallback: (work: Promise<Connection>, value: Connection) => boolean;
  cancelCallback: () => void;
}
export const AuthContext = createContext<Authentication | undefined>(undefined);
export function AuthProvider({ children, callback, loadOptions = true }: { children: ReactNode; callback?: LoginReturn; loadOptions?: boolean }) {
  const [connection, setConnection] = useState<Connection>();
  const current = useRef<Connection | undefined>(undefined);
  // false means spent/cancelled for this document. Never re-exchange a scrubbed
  // callback after StrictMode replay, route remount, disconnect or another login.
  const completion = useRef<Promise<Connection> | false | undefined>(undefined);
  const [options, setOptions] = useState<LoginOptions | undefined>(loadOptions ? undefined : { enabled: false, manual_token: import.meta.env.DEV });
  const [optionsError, setOptionsError] = useState(false); const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!loadOptions) return;
    const controller = new AbortController();
    loginClient(config.apiBaseURL).options(controller.signal).then((value) => { if (!controller.signal.aborted) { setOptions(value); setOptionsError(false); } })
      .catch(() => { if (!controller.signal.aborted) { setOptions(undefined); setOptionsError(true); } });
    return () => controller.abort();
  }, [loadOptions, attempt]);
  const connect = useCallback((value: Connection) => { completion.current = false; current.current = value; setConnection(value); }, []);
  const disconnect = useCallback((expected?: Connection) => {
    if (expected && current.current !== expected) return;
    completion.current = false; current.current = undefined; setConnection(undefined);
  }, []);
  const callbackWork = useCallback(() => {
    if (!callback || callback.kind === "error" || completion.current === false) return;
    if (!completion.current) {
      const work: Promise<Connection> = loginClient(config.apiBaseURL).complete(callback).then(async (token) => {
        if (completion.current !== work) throw new Error("Sign-in was cancelled");
        const session = await createAPIClient(config.apiBaseURL).session(token);
        return { token, session };
      });
      completion.current = work;
    }
    return completion.current;
  }, [callback]);
  const acceptCallback = useCallback((work: Promise<Connection>, value: Connection) => {
    if (completion.current !== work) return false;
    connect(value); return true;
  }, [connect]);
  const cancelCallback = useCallback(() => { completion.current = false; }, []);
  return <AuthContext.Provider value={{ connection, connect, disconnect, options, optionsError, reloadOptions: () => { setOptionsError(false); setAttempt((n) => n + 1); }, callbackWork, acceptCallback, cancelCallback }}>{children}</AuthContext.Provider>;
}
export function useAuthentication() {
  const value = useContext(AuthContext); if (!value) throw new Error("Authentication provider missing"); return value;
}

// Observe only a bearer request owned by this exact connection. The transport
// adds no credentials and an old client's late 401 cannot clear a new account.
export function useConnectionTransport(token: string): typeof fetch {
  const auth = useContext(AuthContext);
  const connection = auth?.connection?.token === token ? auth.connection : undefined;
  const disconnect = auth?.disconnect;
  return useMemo(() => {
    if (!connection || !disconnect) return fetch;
    return async (input, init) => {
      const response = await fetch(input, init);
      const headers = new Headers(init?.headers ?? (typeof Request !== "undefined" && input instanceof Request ? input.headers : undefined));
      if (response.status === 401 && headers.get("Authorization") === `Bearer ${connection.token}`) disconnect(connection);
      return response;
    };
  }, [connection, disconnect]);
}
