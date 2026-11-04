export interface Field {
  id: string;
  label: string;
  type: string;
  required: boolean;
  core_id: boolean;
  enum_values?: string[];
  currency?: string;
  reference_to?: string;
  unique?: boolean;
}
export interface Entity {
  id: string;
  label: string;
  fields: Field[];
}
export interface Component {
  type: string;
  label?: string;
  field_ref?: string;
  entity_ref?: string;
  children?: Component[];
}
export interface Page {
  id: string;
  label: string;
  kind: "page";
  page: { entity_ref: string; components: Component[] };
}
export interface Preview {
  mode: "preview";
  revision: number;
  ir_hash: string;
  can_write: boolean;
  ir: {
    ir_version: string;
    compiler_version: string;
    schema_snapshot: string;
    entities: Entity[];
    application: { app_id: string; definitions: Page[] };
  };
}
export interface Row {
  owner_user_id?: string;
  owner_team_id?: string;
  field_access?: { masked: string[]; writable: string[] };
  id: string;
  version: number;
  values: Record<string, unknown>;
}
export interface Result {
  records: Row[];
  next: string;
}
export const rendererVersion = "1.0.0";
export const registry: Record<
  string,
  { fields?: string[]; children: boolean }
> = {
  section: { children: true },
  tabs: { children: true },
  form: { children: true },
  table: { children: false },
  detail: { children: false },
  text_input: { fields: ["string"], children: false },
  number_input: { fields: ["integer", "decimal"], children: false },
  money_input: { fields: ["money"], children: false },
  date_input: { fields: ["date", "datetime"], children: false },
  select: { fields: ["enum", "bool"], children: false },
  relation_picker: { fields: ["reference"], children: false },
  status_badge: { fields: ["enum", "string", "bool"], children: false },
};
export function object(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}
export const uuid = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v);
const stableID = (v: unknown, prefix: string): v is string =>
  typeof v === "string" &&
  v.length <= 160 &&
  new RegExp(`^${prefix}\\.[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$`).test(v);
export function decodePreview(
  raw: unknown,
  app: string,
  revision: number,
): Preview {
  if (
    !object(raw) ||
    raw.mode !== "preview" ||
    raw.revision !== revision ||
    typeof raw.can_write !== "boolean" ||
    typeof raw.ir_hash !== "string" ||
    !/^[a-f0-9]{64}$/.test(raw.ir_hash) ||
    !object(raw.ir)
  )
    throw new Error("Invalid preview");
  const ir = raw.ir;
  if (
    (ir.ir_version !== rendererVersion && ir.ir_version !== "1.1.0" && ir.ir_version !== "1.2.0" && ir.ir_version !== "1.3.0" && ir.ir_version !== "1.4.0" && ir.ir_version !== "1.5.0") ||
    typeof ir.compiler_version !== "string" ||
    ir.compiler_version.length === 0 ||
    ir.compiler_version.length > 80 ||
    ir.schema_snapshot !== "hybrid-preview-v1" ||
    !object(ir.application) ||
    ir.application.app_id !== app ||
    !Array.isArray(ir.entities) ||
    ir.entities.length > 500 ||
    !Array.isArray(ir.application.definitions) ||
    ir.application.definitions.length > 500
  )
    throw new Error("Unsupported preview version");
  const entities = new Map<string, Entity>();
  const fields = new Set<string>();
  for (const entity of ir.entities) {
    if (
      !object(entity) ||
      !stableID(entity.id, "entity") ||
      typeof entity.label !== "string" ||
      !Array.isArray(entity.fields) ||
      entity.fields.length === 0 ||
      entity.fields.length > 32 ||
      entities.has(entity.id)
    )
      throw new Error("Unsupported entity");
    for (const f of entity.fields) {
      if (
        !object(f) ||
        !stableID(f.id, "field") ||
        fields.has(f.id) ||
        typeof f.label !== "string" ||
        typeof f.type !== "string" ||
        ![
          "string",
          "integer",
          "decimal",
          "money",
          "bool",
          "date",
          "datetime",
          "enum",
          "reference",
        ].includes(f.type) ||
        typeof f.required !== "boolean" ||
        typeof f.core_id !== "boolean" ||
        (f.unique !== undefined && ((ir.ir_version !== "1.4.0" && ir.ir_version !== "1.5.0") || typeof f.unique !== "boolean" || (f.unique && f.core_id)))
      )
        throw new Error("Invalid field");
      if (
        f.type === "enum" &&
        (!Array.isArray(f.enum_values) ||
          !f.enum_values.every((v) => typeof v === "string"))
      )
        throw new Error("Invalid enumeration");
      if (
        f.type === "money" &&
        (typeof f.currency !== "string" || !/^[A-Z]{3}$/.test(f.currency))
      )
        throw new Error("Invalid currency");
      if (f.type === "reference" && !stableID(f.reference_to, "entity"))
        throw new Error("Invalid reference");
      fields.add(f.id);
    }
    const core = entity.fields.filter((f) => f.core_id);
    if (
      core.length > 1 ||
      (core.length === 1 &&
        (core[0].type !== "string" ||
          core[0].required !== true ||
          core[0].id !== `field.${entity.id.slice(7)}.id`))
    )
      throw new Error("Invalid core identifier");
    entities.set(entity.id, entity as unknown as Entity);
  }
  let nodes = 0;
  function component(
    v: unknown,
    entity: Entity,
    depth: number,
    inForm = false,
  ) {
    if (
      !object(v) ||
      typeof v.type !== "string" ||
      !Object.hasOwn(registry, v.type) ||
      ++nodes > 1000 ||
      depth > 8 ||
      (v.label !== undefined && typeof v.label !== "string")
    )
      throw new Error("Unsupported component");
    const spec = registry[v.type];
    if (v.type === "form" && inForm)
      throw new Error("Nested forms are unsupported");
    if (v.entity_ref !== undefined && v.entity_ref !== entity.id)
      throw new Error("Invalid component entity");
    if (spec.fields) {
      const field = entity.fields.find((f) => f.id === v.field_ref);
      if (!field || !spec.fields.includes(field.type))
        throw new Error("Incompatible component field");
    }
    if (v.children !== undefined) {
      if (!spec.children || !Array.isArray(v.children))
        throw new Error("Invalid component children");
      v.children.forEach((c) =>
        component(c, entity, depth + 1, inForm || v.type === "form"),
      );
    }
  }
  const pages: Page[] = [];
  const pageIDs = new Set<string>();
  for (const d of ir.application.definitions) {
    if (!object(d)) throw new Error("Invalid definition");
    if (d.kind !== "page") continue;
    if (
      !stableID(d.id, "page") ||
      pageIDs.has(d.id) ||
      typeof d.label !== "string" ||
      !object(d.page) ||
      typeof d.page.entity_ref !== "string" ||
      !Array.isArray(d.page.components)
    )
      throw new Error("Invalid page");
    const entity = entities.get(d.page.entity_ref);
    if (!entity) throw new Error("Unknown page entity");
    d.page.components.forEach((c) => component(c, entity, 0));
    pageIDs.add(d.id);
    pages.push(d as unknown as Page);
  }
  for (const e of entities.values())
    for (const f of e.fields)
      if (f.reference_to && !entities.has(f.reference_to))
        throw new Error("Unknown reference target");
  return {
    mode: "preview",
    revision,
    ir_hash: raw.ir_hash,
    can_write: raw.can_write,
    ir: {
      ir_version: String(ir.ir_version),
      compiler_version: String(ir.compiler_version),
      schema_snapshot: "hybrid-preview-v1",
      entities: [...entities.values()],
      application: { app_id: app, definitions: pages },
    },
  };
}
export function decodeRow(v: unknown): Row {
  if (
    !object(v) ||
    !uuid(v.id) ||
    !Number.isSafeInteger(v.version) ||
    Number(v.version) < 1 ||
    !object(v.values) ||
    v.owner_user_id !== undefined && !uuid(v.owner_user_id) ||
    v.owner_team_id !== undefined && !uuid(v.owner_team_id)
  )
    throw new Error("Invalid record");
  if (v.field_access !== undefined) {
    const access = v.field_access;
    if (!object(access) || !Array.isArray(access.masked) || !Array.isArray(access.writable) ||
      access.masked.length > 32 || access.writable.length > 32 ||
      [...access.masked, ...access.writable].some((x) => typeof x !== "string") ||
      new Set(access.masked).size !== access.masked.length || new Set(access.writable).size !== access.writable.length ||
      access.masked.some((x) => Object.hasOwn(v.values as object, x) || (access.writable as unknown[]).includes(x)))
      throw new Error("Invalid field access");
  }
  return v as unknown as Row;
}
export function decodeResult(v: unknown): Result {
  if (
    !object(v) ||
    !Array.isArray(v.records) ||
    v.records.length > 100 ||
    typeof v.next !== "string" ||
    v.next.length > 43691
  )
    throw new Error("Invalid records");
  return { records: v.records.map(decodeRow), next: v.next };
}
export function display(v: unknown): string {
  if (v === null || v === undefined) return "—";
  if (
    object(v) &&
    typeof v.amount === "string" &&
    typeof v.currency === "string"
  )
    return `${v.amount} ${v.currency}`;
  return ["string", "number", "boolean"].includes(typeof v)
    ? String(v)
    : "Unsupported value";
}
