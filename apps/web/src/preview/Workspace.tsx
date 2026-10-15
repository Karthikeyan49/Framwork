import { useEffect, useRef, useState } from "react";
import { APIError } from "../api/client";
import { CommandOutcomeUnknown } from "../api/preview";
import { previewClient } from "../api/preview";
import { config } from "../config";
import {
  display,
  object,
  type Component,
  type Entity,
  type Field,
  type Page,
  type Preview,
  type Row,
} from "./contracts";

type Client = ReturnType<typeof previewClient>;
function message(e: unknown) {
  if (e instanceof APIError) {
    if (e.status === 409)
      return "This record changed. Reload records before editing again.";
    if (e.status === 403)
      return "Your permissions do not allow this operation.";
    if (e.status === 401)
      return "Your session expired. Disconnect and connect again.";
    if (e.status === 400)
      return "Check the field values or query limits and try again.";
    if (e.status === 422)
      return "This draft has compiler errors. Correct the draft before previewing.";
  }
  return "The preview could not be loaded. Check the service and try again.";
}
export function formValues(
  entity: Entity,
  inputs: Record<string, string>,
  initial?: Row,
) {
  const values: Record<string, unknown> = { ...initial?.values };
  for (const field of entity.fields) {
    if (field.core_id) {
      delete values[field.id];
      continue;
    }
    if (!Object.hasOwn(inputs, field.id)) continue;
    const text = inputs[field.id];
    if (text === "") {
      if (field.required) throw new Error(`${field.label} is required.`);
      values[field.id] = null;
      continue;
    }
    if (field.type === "integer") {
      const n = Number(text);
      if (!/^-?\d+$/.test(text) || !Number.isSafeInteger(n))
        throw new Error(`${field.label} must be a safe whole number.`);
      values[field.id] = n;
    } else if (field.type === "money")
      values[field.id] = { amount: text, currency: field.currency };
    else if (field.type === "bool") values[field.id] = text === "true";
    else if (field.type === "datetime") {
      if (
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/.test(
          text,
        )
      )
        throw new Error(
          `${field.label} requires an RFC3339 timestamp with a timezone.`,
        );
      values[field.id] = text;
    } else values[field.id] = text;
  }
  return values;
}
function inputsFor(row?: Row): Record<string, string> {
  return Object.fromEntries(
    Object.entries(row?.values ?? {}).map(([key, value]) => [
      key,
      object(value)
        ? String(value.amount ?? "")
        : value === null
          ? ""
          : String(value),
    ]),
  );
}
export function ApplicationWorkspace({
  token,
  tenant,
  pendingCommands: retainedCommands,
  onPendingCommand,
  onCommandBusy,
}: {
  token: string;
  tenant: string;
  pendingCommands?: Record<string, string>;
  onPendingCommand?: (scope: string, key: string) => void;
  onCommandBusy?: (key: string, busy: boolean) => void;
}) {
  const [app, setApp] = useState("app.contacts");
  const [revision, setRevision] = useState("1");
  const [loaded, setLoaded] = useState<{ preview: Preview; client: Client }>();
  const [page, setPage] = useState("");
  const [writePending, setWritePending] = useState(false);
  const [blockedWrites, setBlockedWrites] = useState<Record<string, boolean>>(
    {},
  );
  const [localPendingCommands, setPendingCommands] = useState<Record<string, string>>({});
  const pendingCommands = retainedCommands ?? localPendingCommands;
  const scopeKey = loaded
    ? tenant +
      ":" +
      app +
      ":" +
      loaded.preview.revision +
      ":" +
      loaded.preview.ir_hash
    : "";
  const writeKey =
    scopeKey + ":" + (selectedEntity(loaded?.preview, page) ?? "");
  function blockWrite(blocked: boolean) {
    setBlockedWrites((old) => ({ ...old, [writeKey]: blocked }));
  }
  function setPendingCommand(key: string) {
    if (onPendingCommand) onPendingCommand(writeKey, key);
    else setPendingCommands((old) => ({ ...old, [writeKey]: key }));
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => request.current?.abort(), []);
  function clear() {
    if (writePending) return;
    request.current?.abort();
    setLoaded(undefined);
    setError("");
    setBusy(false);
  }
  async function load() {
    if (writePending) return;
    clear();
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    try {
      const client = previewClient(
        config.apiBaseURL,
        token,
        tenant,
        app.trim(),
        Number(revision),
      );
      const preview = await client.descriptor(controller.signal);
      if (!controller.signal.aborted) {
        setLoaded({ preview, client });
        setPage(preview.ir.application.definitions[0]?.id ?? "");
      }
    } catch (e) {
      if (!controller.signal.aborted) setError(message(e));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  const selected = loaded?.preview.ir.application.definitions.find(
    (p) => p.id === page,
  );
  return (
    <section className="preview">
      <h2>Application preview</h2>
      <p>
        Explore a compiled draft using isolated preview records. These records
        are separate from production.
      </p>
      <form
        className="preview-selector"
        onSubmit={(e) => {
          e.preventDefault();
          void load();
        }}
      >
        <label>
          Application ID
          <input
            disabled={writePending}
            value={app}
            onChange={(e) => {
              clear();
              setApp(e.target.value);
            }}
            required
            pattern="app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*"
          />
        </label>
        <label>
          Draft revision
          <input
            type="number"
            min="1"
            max="1000000000"
            step="1"
            disabled={writePending}
            value={revision}
            onChange={(e) => {
              clear();
              setRevision(e.target.value);
            }}
            required
          />
        </label>
        <button disabled={busy || writePending}>
          {busy ? "Loading preview…" : "Load preview"}
        </button>
      </form>
      {error && <p role="alert">{error}</p>}
      {loaded && (
        <>
          <p className="badge">
            Development preview · draft {loaded.preview.revision} ·{" "}
            {loaded.preview.ir_hash.slice(0, 12)}
          </p>
          {!loaded.preview.can_write && <p>Read-only access</p>}
          <nav className="page-nav" aria-label="Application pages">
            {loaded.preview.ir.application.definitions.map((p) => (
              <button
                key={p.id}
                disabled={writePending}
                aria-current={page === p.id ? "page" : undefined}
                onClick={() => setPage(p.id)}
              >
                {p.label}
              </button>
            ))}
          </nav>
          {selected ? (
            <EntityPage
              key={`${loaded.preview.ir_hash}:${page}`}
              page={selected}
              preview={loaded.preview}
              client={loaded.client}
              onWritePending={setWritePending}
              writeBlocked={!!blockedWrites[writeKey]}
              blockWrite={blockWrite}
              pendingCommand={pendingCommands[writeKey] ?? ""}
              setPendingCommand={setPendingCommand}
              onCommandBusy={onCommandBusy}
            />
          ) : (
            <p>This application has no pages.</p>
          )}
        </>
      )}
    </section>
  );
}
function selectedEntity(preview: Preview | undefined, page: string) {
  return preview?.ir.application.definitions.find((p) => p.id === page)?.page
    .entity_ref;
}
function RelationPicker({
  reloadEpoch,
  field,
  value,
  set,
  preview,
  client,
  disabled,
  id,
  label,
}: {
  reloadEpoch: number;
  label: string;
  id: string;
  field: Field;
  value: string;
  set: (value: string) => void;
  preview: Preview;
  client: Client;
  disabled: boolean;
}) {
  const [rows, setRows] = useState<Row[]>([]);
  const [status, setStatus] = useState("Loading choices…");
  useEffect(() => {
    const controller = new AbortController();
    const target = preview.ir.entities.find(
      (e) => e.id === field.reference_to,
    )!;
    const id = target.fields.find((f) => f.core_id)?.id;
    const label =
      target.fields.find((f) => f.type === "string" && !f.core_id)?.id ??
      target.fields[0].id;
    client
      .query(
        target.id,
        [label, ...(id && label !== id ? [id] : [])],
        "",
        controller.signal,
      )
      .then((result) => {
        if (!controller.signal.aborted) {
          setRows(result.records);
          setStatus(
            result.next
              ? "Showing the first 20 choices. Use a record ID for other choices."
              : "",
          );
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) setStatus(message(e));
      });
    return () => controller.abort();
  }, [client, field, preview, reloadEpoch]);
  return (
    <>
      <input
        id={id}
        aria-label={label}
        list={`choices-${id}`}
        value={value}
        onChange={(e) => set(e.target.value)}
        required={field.required}
        disabled={disabled}
        pattern="[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
        placeholder="Choose a record or enter its ID"
      />
      <datalist id={`choices-${id}`}>
        {rows.map((r) => (
          <option key={r.id} value={r.id}>
            {Object.values(r.values)
              .filter((v) => v !== r.id)
              .map(display)
              .join(" · ")}
          </option>
        ))}
      </datalist>
      {status && <small role="status">{status}</small>}
    </>
  );
}
function EntityPage({
  onCommandBusy,
  pendingCommand,
  setPendingCommand,
  writeBlocked,
  blockWrite,
  onWritePending,
  page,
  preview,
  client,
}: {
  onCommandBusy?: (key: string, busy: boolean) => void;
  pendingCommand: string;
  setPendingCommand: (key: string) => void;
  onWritePending: (pending: boolean) => void;
  writeBlocked: boolean;
  blockWrite: (blocked: boolean) => void;
  page: Page;
  preview: Preview;
  client: Client;
}) {
  const entity = preview.ir.entities.find(
    (e) => e.id === page.page.entity_ref,
  )!;
  const needsReload = writeBlocked || Boolean(pendingCommand);
  const setNeedsReload = blockWrite;
  const [reloadEpoch, setReloadEpoch] = useState(0);
  const [rows, setRows] = useState<Row[]>([]);
  const [next, setNext] = useState("");
  const [selected, setSelected] = useState<Row>();
  const [inputs, setInputs] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const queries = useRef<AbortController | undefined>(undefined);
  const writes = useRef<AbortController | undefined>(undefined);
  useEffect(() => {
    const controller = new AbortController();
    queries.current = controller;
    client
      .query(
        entity.id,
        entity.fields.map((f) => f.id),
        "",
        controller.signal,
      )
      .then((result) => {
        if (!controller.signal.aborted) {
          setRows(result.records);
          setNext(result.next);
          setBusy(false);
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) {
          setError(message(e));
          setBusy(false);
        }
      });
    return () => {
      controller.abort();
      queries.current?.abort();
      writes.current?.abort();
    };
  }, [client, entity]);
  async function load(after = "") {
    queries.current?.abort();
    const controller = new AbortController();
    queries.current = controller;
    setBusy(true);
    setError("");
    try {
      const result = await client.query(
        entity.id,
        entity.fields.map((f) => f.id),
        after,
        controller.signal,
      );
      if (!controller.signal.aborted) {
        setRows(result.records);
        setNext(result.next);
        setNeedsReload(Boolean(pendingCommand));
        setSelected(undefined);
        setInputs({});
        setConfirmDelete(false);
        setReloadEpoch((old) => old + 1);
        return true;
      }
    } catch (e) {
      if (!controller.signal.aborted) {
        setNeedsReload(true);
        setError(message(e));
      }
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
    return false;
  }
  async function command(operation: "create" | "update" | "delete") {
    if (!preview.can_write || saving || busy || needsReload) return;
    let values: Record<string, unknown>;
    try {
      values =
        operation === "delete" ? {} : formValues(entity, inputs, selected);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Check the field values.");
      return;
    }
    writes.current?.abort();
    const controller = new AbortController();
    writes.current = controller;
    setNeedsReload(true);
    setSaving(true);
    onWritePending(true);
    setError("");
    setNotice("");
    // Retain the key before dispatch, above the workspace's unmount boundary.
    // Cancellation/disconnect cannot establish that the server rolled back.
    const key = crypto.randomUUID();
    setPendingCommand(key);
    onCommandBusy?.(key, true);
    try {
      const row = await client.command(
        entity.id,
        operation,
        values,
        selected,
        controller.signal,
        key,
      );
      setPendingCommand("");
      if (!controller.signal.aborted) {
        const reloaded = await load();
        setNeedsReload(!reloaded);
        setNotice(
          !reloaded
            ? "The change was saved, but records could not be reloaded. Reload and inspect records before another write."
            : operation === "delete"
              ? "Record deleted."
              : `Record saved · version ${row.version}.`,
        );
      }
    } catch (e) {
      // The parent owns recovery facts even after this page is unmounted.
      if (e instanceof APIError && e.status < 500) setPendingCommand("");
      if (!controller.signal.aborted) {
        if (e instanceof CommandOutcomeUnknown) {
          setPendingCommand(e.commandKey);
          setNeedsReload(true);
          setError(
            "The write outcome is uncertain. Check the command receipt before another write. An unknown receipt does not prove the command failed.",
          );
        } else if (e instanceof APIError) {
          if (e.status === 409) setNeedsReload(true);
          if (e.status !== 409) setNeedsReload(false);
          setError(message(e));
        } else {
          setNeedsReload(true);
          setError(message(e));
        }
      }
    } finally {
      onCommandBusy?.(key, false);
      onWritePending(false);
      if (!controller.signal.aborted) setSaving(false);
    }
  }
  function select(row?: Row) {
    setSelected(row);
    setInputs(inputsFor(row));
    setConfirmDelete(false);
    if (!needsReload) setError("");
    setNotice("");
  }
  async function reconcile() {
    if (!pendingCommand || saving || busy) return;
    setSaving(true);
    setError("");
    try {
      const receipt = await client.receipt(pendingCommand);
      if (receipt.state === "COMMITTED") {
        setPendingCommand("");
        const reloaded = await load();
        // load captured the old pending key; release the gate only after both
        // a committed owned receipt and the fresh read have been confirmed.
        setNeedsReload(!reloaded);
        setNotice(`Command committed · version ${receipt.record_version}.`);
      } else {
        setError("The receipt is still unknown. Keep this command key and check again; do not repeat the write.");
      }
    } catch (e) {
      setError(message(e));
    } finally {
      setSaving(false);
    }
  }
  function render(c: Component, key: string): React.ReactNode {
    const children = c.children?.map((child, i) =>
      render(child, `${key}.${i}`),
    );
    if (c.type === "section")
      return (
        <section className="card dynamic-section" key={key}>
          {c.label && <h3>{c.label}</h3>}
          {children}
        </section>
      );
    if (c.type === "tabs")
      return (
        <Tabs
          key={key}
          label={c.label}
          components={c.children ?? []}
          render={(child, i) => render(child, `${key}.${i}`)}
        />
      );
    if (c.type === "form")
      return (
        <form
          key={key}
          onSubmit={(e) => {
            e.preventDefault();
            void command(selected ? "update" : "create");
          }}
        >
          <h3>{selected ? "Edit record" : "New record"}</h3>
          {children}
          <button
            disabled={!preview.can_write || saving || busy || needsReload}
          >
            {saving ? "Saving…" : selected ? "Save changes" : "Create record"}
          </button>
          {selected && (
            <button
              type="button"
              disabled={saving || busy}
              onClick={() => select()}
            >
              New record
            </button>
          )}
        </form>
      );
    if (c.type === "table")
      return (
        <div className="table-scroll" key={key}>
          <h3>{entity.label} records</h3>
          <table>
            <thead>
              <tr>
                {entity.fields.map((f) => (
                  <th key={f.id} scope="col">
                    {f.label}
                  </th>
                ))}
                <th scope="col">Selection</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id}>
                  {entity.fields.map((f) => (
                    <td key={f.id}>{display(r.values[f.id])}</td>
                  ))}
                  <td>
                    <button
                      type="button"
                      disabled={busy || saving}
                      onClick={() => select(r)}
                    >
                      Open record
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!busy && !error && rows.length === 0 && <p>No records yet.</p>}
          <div className="actions">
            <button
              type="button"
              disabled={!next || busy || saving}
              onClick={() => void load(next)}
            >
              Next page
            </button>
          </div>
        </div>
      );
    if (c.type === "detail")
      return (
        <section key={key}>
          <h3>Record details</h3>
          {selected ? (
            <>
              <p>Version {selected.version}</p>
              <p>Record ID {selected.id}</p>
              <dl>
                {entity.fields.map((f) => (
                  <div key={f.id}>
                    <dt>{f.label}</dt>
                    <dd>{display(selected.values[f.id])}</dd>
                  </div>
                ))}
              </dl>
              {preview.can_write && (
                <>
                  {confirmDelete ? (
                    <div className="actions">
                      <span>Delete this preview record?</span>
                      <button
                        type="button"
                        disabled={saving || busy || needsReload}
                        onClick={() => void command("delete")}
                      >
                        Confirm delete
                      </button>
                      <button
                        type="button"
                        disabled={saving}
                        onClick={() => setConfirmDelete(false)}
                      >
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      disabled={busy || saving || needsReload}
                      onClick={() => setConfirmDelete(true)}
                    >
                      Delete record
                    </button>
                  )}
                </>
              )}
            </>
          ) : (
            <p>Open a record to see its details.</p>
          )}
        </section>
      );
    const f = entity.fields.find((field) => field.id === c.field_ref)!;
    if (c.type === "status_badge")
      return (
        <p key={key} className="badge">
          {c.label ?? f.label}: {display(selected?.values[f.id])}
        </p>
      );
    const value = inputs[f.id] ?? "";
    const disabled =
      !preview.can_write || f.core_id || saving || busy || needsReload;
    const set = (value: string) =>
      setInputs((old) => ({ ...old, [f.id]: value }));
    const id = `input-${key}`;
    return (
      <label key={key} htmlFor={id}>
        {c.label ?? f.label}
        {f.required && <span aria-hidden="true"> *</span>}
        {c.type === "relation_picker" ? (
          <span>
            <RelationPicker
              reloadEpoch={reloadEpoch}
              id={id}
              label={c.label ?? f.label}
              field={f}
              value={value}
              set={set}
              preview={preview}
              client={client}
              disabled={disabled}
            />
          </span>
        ) : c.type === "select" ? (
          <select
            id={id}
            aria-label={c.label ?? f.label}
            value={value}
            required={f.required}
            disabled={disabled}
            onChange={(e) => set(e.target.value)}
          >
            <option value="">Choose…</option>
            {(f.type === "bool"
              ? ["true", "false"]
              : (f.enum_values ?? [])
            ).map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        ) : (
          <input
            id={id}
            aria-label={c.label ?? f.label}
            value={value}
            required={f.required}
            disabled={disabled}
            maxLength={f.type === "string" ? 4096 : 64}
            type={
              c.type === "date_input"
                ? f.type === "datetime"
                  ? "text"
                  : "date"
                : c.type === "number_input" && f.type === "integer"
                  ? "number"
                  : "text"
            }
            step={f.type === "integer" ? 1 : undefined}
            inputMode={
              ["integer", "decimal", "money"].includes(f.type)
                ? "decimal"
                : undefined
            }
            onChange={(e) => set(e.target.value)}
          />
        )}
        {f.currency && <small>Currency: {f.currency}</small>}
      </label>
    );
  }
  return (
    <section aria-label={page.label}>
      <h2>{page.label}</h2>
      <div className="actions">
        <button
          type="button"
          disabled={busy || saving}
          onClick={() => void load()}
        >
          Reload records
        </button>
      </div>
      {busy && <p role="status">Loading records…</p>}
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      {pendingCommand && (
        <div>
          <p>Pending command: <code>{pendingCommand}</code></p>
          <button type="button" disabled={busy || saving} onClick={() => void reconcile()}>
            Check command receipt
          </button>
        </div>
      )}
      {needsReload && !saving && (
        <p role="status">
          A write requires review. Reload records and inspect them before
          writing again.
        </p>
      )}
      {rows.length > 0 && !hasTable(page.page.components) && (
        <label>
          Open record
          <select
            value={selected?.id ?? ""}
            onChange={(e) => select(rows.find((r) => r.id === e.target.value))}
            disabled={busy || saving}
          >
            <option value="">Choose a record…</option>
            {rows.map((r) => (
              <option value={r.id} key={r.id}>
                {Object.values(r.values).map(display).join(" · ")}
              </option>
            ))}
          </select>
        </label>
      )}
      {page.page.components.map((c, i) => render(c, String(i)))}
    </section>
  );
}
function Tabs({
  components,
  render,
  label,
}: {
  components: Component[];
  render: (c: Component, i: number) => React.ReactNode;
  label?: string;
}) {
  const [active, setActive] = useState(0);
  return (
    <section aria-label={label ?? "Page sections"}>
      <div className="actions">
        {components.map((c, i) => (
          <button
            key={i}
            type="button"
            aria-pressed={active === i}
            onClick={() => setActive(i)}
          >
            {c.label ?? `Section ${i + 1}`}
          </button>
        ))}
      </div>
      {components[active] && render(components[active], active)}
    </section>
  );
}

function hasTable(components: Component[]): boolean {
  return components.some(
    (c) => c.type === "table" || hasTable(c.children ?? []),
  );
}
