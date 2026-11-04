import { conditionOperators, decodeRoleDefinition, parseConditionValue, roleSchema, scopeKinds, type RoleContract, type RoleDefinition, type RoleGrant, type RoleScope, type ScopeKind } from "./roleContracts";

export interface ScopeDraft { kind: ScopeKind; field?: string; op?: string; value?: string; children?: ScopeDraft[] }
export interface GrantDraft { effect: "ALLOW" | "DENY"; actions: string[]; resource: RoleGrant["resource"]; scope?: ScopeDraft }
export const emptyGrant = (): GrantDraft => ({ effect: "ALLOW", actions: [], resource: { app_id: "*", entity_id: "*", field_id: "*" } });
function draftScope(scope: RoleScope): ScopeDraft {
  return { kind: scope.kind, ...(scope.condition ? { field: scope.condition.field, op: scope.condition.op, value: scope.condition.value === undefined ? "" : JSON.stringify(scope.condition.value) } : {}), ...(scope.children ? { children: scope.children.map(draftScope) } : {}) };
}
export const draftGrants = (definition: RoleDefinition): GrantDraft[] => definition.grants.map((grant) => ({ ...grant, actions: [...grant.actions], resource: { ...grant.resource }, ...(grant.scope ? { scope: draftScope(grant.scope) } : {}) }));
function compileScope(scope: ScopeDraft): RoleScope {
  if (scope.kind === "AND" || scope.kind === "OR") return { kind: scope.kind, children: (scope.children ?? []).map(compileScope) };
  if (scope.kind === "CONDITION") return { kind: scope.kind, condition: { field: scope.field ?? "", op: scope.op ?? "eq", ...(scope.op === "is_null" ? {} : { value: parseConditionValue(scope.value ?? "") }) } };
  return { kind: scope.kind };
}
export function compileDraft(name: string, grants: GrantDraft[], contract: RoleContract): RoleDefinition {
  return decodeRoleDefinition({ schema_version: roleSchema, name, grants: grants.map((grant) => ({ ...grant, ...(grant.scope ? { scope: compileScope(grant.scope) } : {}) })) }, name, contract);
}
function ScopeEditor({ value, onChange, label, depth = 0 }: { value: ScopeDraft; onChange: (v: ScopeDraft) => void; label: string; depth?: number }) {
  const combined = value.kind === "AND" || value.kind === "OR";
  return <fieldset className="role-scope"><legend>{label}</legend><label>{label} kind<select value={value.kind} onChange={(e) => { const kind = e.target.value as ScopeKind; onChange({ kind, ...(kind === "CONDITION" ? { field: "", op: "eq", value: "" } : {}), ...(kind === "AND" || kind === "OR" ? { children: [{ kind: "NONE" }] } : {}) }); }}>{scopeKinds.filter((kind) => depth < 6 || kind !== "AND" && kind !== "OR").map((kind) => <option key={kind}>{kind}</option>)}</select></label>
    {value.kind === "CONDITION" && <><label>{label} field<input value={value.field ?? ""} maxLength={160} placeholder="field.invoice.amount" onChange={(e) => onChange({ ...value, field: e.target.value })} /></label><label>{label} operator<select value={value.op ?? "eq"} onChange={(e) => onChange({ ...value, op: e.target.value })}>{conditionOperators.map((op) => <option key={op}>{op}</option>)}</select></label>{value.op !== "is_null" && <label>{label} JSON value<textarea value={value.value ?? ""} maxLength={1024} placeholder={'"100.00"'} onChange={(e) => onChange({ ...value, value: e.target.value })}/></label>}<p>Use quoted strings for exact money and decimals. Invalid JSON blocks publication.</p></>}
    {combined && <>{(value.children ?? []).map((child, i) => <div key={i}><ScopeEditor value={child} depth={depth + 1} label={`${label} child ${i + 1}`} onChange={(next) => onChange({ ...value, children: value.children!.map((c, j) => j === i ? next : c) })}/><button type="button" onClick={() => onChange({ ...value, children: value.children!.filter((_, j) => j !== i) })}>Remove {label} child {i + 1}</button></div>)}<button type="button" disabled={(value.children?.length ?? 0) >= 8 || depth >= 6} onClick={() => onChange({ ...value, children: [...(value.children ?? []), { kind: "NONE" }] })}>Add {label} child</button></>}
  </fieldset>;
}
export function RoleEditor({ value, onChange, contract, disabled }: { value: GrantDraft[]; onChange: (grants: GrantDraft[]) => void; contract: RoleContract; disabled: boolean }) {
  const update = (i: number, grant: GrantDraft) => onChange(value.map((g, j) => j === i ? grant : g));
  return <fieldset disabled={disabled}><legend>Role grants</legend><p>Configured roles have only these grants. Empty grants deny all access. Wildcards apply to every matching resource. DENY overrides ALLOW.</p>{value.map((grant, i) => {
    const scoped = grant.effect === "ALLOW" && grant.actions.some((action) => contract.actions.find((a) => a.name === action)?.scope_required);
    return <fieldset key={i}><legend>Grant {i + 1}</legend><label>Grant {i + 1} effect<select value={grant.effect} onChange={(e) => { const effect = e.target.value as GrantDraft["effect"]; update(i, { ...grant, effect, scope: effect === "ALLOW" && grant.actions.some((action) => contract.actions.find((a) => a.name === action)?.scope_required) ? { kind: "NONE" } : undefined }); }}><option>ALLOW</option><option>DENY</option></select></label>
      <details><summary>Grant {i + 1} capabilities ({grant.actions.length} selected)</summary><div className="role-capabilities">{contract.actions.map((action) => <label key={action.name}><input type="checkbox" checked={grant.actions.includes(action.name)} disabled={grant.effect === "ALLOW" && !action.allow_supported} onChange={(e) => { const actions = e.target.checked ? [...grant.actions, action.name] : grant.actions.filter((a) => a !== action.name); const needsScope = grant.effect === "ALLOW" && actions.some((a) => contract.actions.find((d) => d.name === a)?.scope_required); update(i, { ...grant, actions, scope: needsScope ? grant.scope ?? { kind: "NONE" } : undefined }); }}/>{action.name}{grant.effect === "ALLOW" && !action.allow_supported ? " (reserved)" : action.scope_required ? " (record scope)" : ""}</label>)}</div></details>
      <p>Use separate ALLOW grants for capabilities with and without record scopes.</p>{(["app_id", "entity_id", "field_id"] as const).map((key) => <label key={key}>Grant {i + 1} {key}<input maxLength={160} value={grant.resource[key]} onChange={(e) => update(i, { ...grant, resource: { ...grant.resource, [key]: e.target.value } })}/></label>)}
      {scoped && <ScopeEditor label={`Grant ${i + 1} scope`} value={grant.scope ?? { kind: "NONE" }} onChange={(scope) => update(i, { ...grant, scope })}/>}<button type="button" onClick={() => onChange(value.filter((_, j) => i !== j))}>Remove grant {i + 1}</button>
    </fieldset>;
  })}<button type="button" disabled={value.length >= contract.limits.grants} onClick={() => onChange([...value, emptyGrant()])}>Add grant</button></fieldset>;
}
