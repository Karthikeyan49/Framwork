import { useContext, useRef, useState, useEffect } from "react";
import { Link, useNavigate } from "react-router-dom";
import { config } from "../config";
import { RecoveryContext } from "../administration/Recovery";
import { loginClient } from "./client";
import { useAuthentication } from "./Session";

export function ProviderSignIn() {
  const { options, optionsError, reloadOptions } = useAuthentication();
  const recovery = useContext(RecoveryContext);
  const [busy, setBusy] = useState(false); const [error, setError] = useState(false);
  const sending = useRef(false); const controller = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => controller.current?.abort(), []);
  const pending = !!recovery && (!!Object.keys(recovery.commandJournal).length || !!Object.keys(recovery.administrationJournal).length);
  async function start() {
    if (sending.current) return; sending.current = true; setBusy(true); setError(false);
    const request = new AbortController(); controller.current = request;
    try { const location = await loginClient(config.apiBaseURL).start(request.signal); if (!request.signal.aborted) window.location.assign(location); }
    catch { if (!request.signal.aborted) setError(true); }
    finally { sending.current = false; if (!request.signal.aborted) setBusy(false); }
  }
  return <>
    {!options && !optionsError && <p role="status">Checking sign-in options…</p>}
    {optionsError && <><p role="alert">Sign-in options could not be loaded.</p><button type="button" onClick={reloadOptions}>Retry sign-in options</button></>}
    {options?.enabled && <><h2>Sign in with your identity provider</h2><p>Use your organization’s sign-in. Workspace permissions are checked by this service.</p>{pending && <p role="alert">This tab holds pending operation IDs. Keep those IDs before leaving. Provider sign-in reloads the application and clears this tab’s recovery journal.</p>}<button type="button" disabled={busy} onClick={() => void start()}>{busy ? "Opening sign-in…" : "Sign in"}</button></>}
    {options && !options.enabled && !options.manual_token && <p role="alert">Sign-in is not configured. Contact your administrator.</p>}
    {error && <p role="alert">Sign-in could not start. Check service availability and start a new sign-in.</p>}
  </>;
}

export function SignInCallback() {
  const { callbackWork, acceptCallback, cancelCallback } = useAuthentication();
  const navigate = useNavigate(); const [error, setError] = useState(false);
  useEffect(() => {
    let cancelled = false; const work = callbackWork();
    if (!work) { queueMicrotask(() => { if (!cancelled) setError(true); }); return () => { cancelled = true; }; }
    // Cleanup only removes this view's observer. Aborting or repeating the
    // consumed flow on StrictMode replay would lose its only successful reply.
    work.then((value) => { if (!cancelled) { if (acceptCallback(work, value)) void navigate("/access", { replace: true }); else setError(true); } })
      .catch(() => { if (!cancelled) setError(true); });
    return () => { cancelled = true; };
  }, [callbackWork, acceptCallback, navigate]);
  return <><h1>Completing sign-in</h1>{error ? <p role="alert">Sign-in was not completed or your account has no access. Start a new sign-in.</p> : <p role="status">Verifying your identity and workspace access…</p>}<Link to="/access" onClick={cancelCallback}>Return to workspace access</Link></>;
}
