import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import source from "../../../../contracts/examples/contacts-draft-v1.json";
import { ApplicationWorkspace, formValues } from "./Workspace";
import { decodePreview, type Preview } from "./contracts";

const tenant = "11111111-1111-1111-1111-111111111111";
function fixture(write = true): unknown {
  const doc = structuredClone(source);
  return {
    mode: "preview",
    revision: 1,
    ir_hash: "a".repeat(64),
    can_write: write,
    ir: {
      ir_version: "1.0.0",
      compiler_version: "1.0.0",
      schema_snapshot: "hybrid-preview-v1",
      application: doc,
      entities: doc.definitions
        .filter((d) => d.entity)
        .map((d) => ({
          id: d.id,
          label: d.label,
          fields: doc.definitions
            .filter((f) => f.field?.entity_ref === d.id)
            .map((f) => ({
              id: f.id,
              label: f.label,
              ...f.field,
              core_id: f.id.endsWith(".id"),
            })),
        })),
    },
  };
}
function preview(write = true) {
  return decodePreview(fixture(write), "app.contacts", 1);
}
const response = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status });
async function load() {
  await userEvent.click(screen.getByRole("button", { name: "Load preview" }));
  await screen.findByRole("heading", { name: "Contact editor" });
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Reload records" }),
    ).toBeEnabled(),
  );
}

describe("versioned approved component contract", () => {
  it("accepts a compiled descriptor and rejects wrong scope/version and incompatible or script components", () => {
    expect(preview().ir.application.definitions).toHaveLength(1);
    for (const change of [
      (p: Preview) => {
        p.ir.ir_version = "9";
      },
      (p: Preview) => {
        p.ir.application.app_id = "app.foreign";
      },
      (p: Preview) => {
        p.ir.application.definitions[0].page.components = [{ type: "script" }];
      },
      (p: Preview) => {
        p.ir.application.definitions[0].page.components = [
          { type: "money_input", field_ref: "field.contact.name" },
        ];
      },
      (p: Preview) => {
        p.ir.application.definitions[0].page.components = [
          { type: "__proto__" },
        ];
      },
    ]) {
      const p = preview();
      change(p);
      expect(() => decodePreview(p, "app.contacts", 1)).toThrow();
    }
    expect(() => decodePreview(fixture(), "app.contacts", 2)).toThrow();
  });
  it("preserves exact money and untouched values while excluding immutable IDs", () => {
    const entity = preview().ir.entities[0];
    const values = formValues(
      entity,
      {
        "field.contact.name": "Changed",
        "field.contact.balance": "0.123456789012345678",
      },
      {
        id: tenant,
        version: 2,
        values: {
          "field.contact.id": tenant,
          "field.contact.status": "active",
        },
      },
    );
    expect(values).toEqual({
      "field.contact.name": "Changed",
      "field.contact.status": "active",
      "field.contact.balance": {
        amount: "0.123456789012345678",
        currency: "USD",
      },
    });
    expect(() =>
      formValues(
        {
          ...entity,
          fields: [
            {
              id: "field.x.number",
              label: "Number",
              type: "integer",
              required: true,
              core_id: false,
            },
          ],
        },
        { "field.x.number": "9007199254740992" },
      ),
    ).toThrow();
  });
  it("preserves nanosecond timestamps and requires explicit timezone without normalizing invalid dates", () => {
    const entity = {
      id: "entity.x",
      label: "X",
      fields: [
        {
          id: "field.x.time",
          label: "Time",
          type: "datetime",
          required: true,
          core_id: false,
        },
      ],
    };
    const timestamp = "2026-10-11T12:00:00.123456789Z";
    expect(
      formValues(entity, { "field.x.time": timestamp })["field.x.time"],
    ).toBe(timestamp);
    expect(
      formValues(entity, { "field.x.time": "2026-02-30T12:00:00Z" })[
        "field.x.time"
      ],
    ).toBe("2026-02-30T12:00:00Z"); // strict calendar validation belongs to Go; never repair invalid input
    expect(() =>
      formValues(entity, { "field.x.time": "2026-10-11T12:00:00" }),
    ).toThrow("timezone");
  });
});
it("renders metadata as text, typed fields, empty table and read-only capability hints", async () => {
  const data = preview(false);
  data.ir.application.definitions[0].label = "<script>bad()</script>";
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) =>
    response(String(url).endsWith("/query") ? { records: [], next: "" } : data),
  );
  const { container } = render(
    <ApplicationWorkspace token="test-token" tenant={tenant} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Load preview" }));
  expect(
    await screen.findByRole("heading", { name: "<script>bad()</script>" }),
  ).toBeInTheDocument();
  expect(container.querySelector("script")).toBeNull();
  expect(await screen.findByText("No records yet.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Create record" })).toBeDisabled();
  expect(screen.getByLabelText("Parent contact")).toBeDisabled();
});
it("sends a scoped typed create command and reports safe errors without exposing driver details", async () => {
  const transport = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (url, options) => {
      if (String(url).endsWith("/commands"))
        return new Response("private driver error", { status: 403 });
      return response(
        options?.method === "POST" ? { records: [], next: "" } : fixture(),
      );
    });
  render(<ApplicationWorkspace token="test-token" tenant={tenant} />);
  await load();
  await userEvent.type(screen.getByLabelText("Name"), "Alice");
  await userEvent.type(screen.getByLabelText("Balance"), "12.30");
  await userEvent.click(screen.getByRole("button", { name: "Create record" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("permissions");
  const call = transport.mock.calls.find(([url]) =>
    String(url).endsWith("/commands"),
  )!;
  expect(JSON.parse(String(call[1]?.body))).toEqual({
    operation: "create",
    values: {
      "field.contact.name": "Alice",
      "field.contact.balance": { amount: "12.30", currency: "USD" },
    },
  });
  expect(call[1]?.credentials).toBe("omit");
  expect(screen.queryByText("private driver error")).not.toBeInTheDocument();
});
it("suppresses aborted descriptors after the draft selection changes", async () => {
  let resolve: (r: Response) => void = () => {};
  vi.spyOn(globalThis, "fetch").mockImplementation(
    () =>
      new Promise<Response>((r) => {
        resolve = r;
      }),
  );
  render(<ApplicationWorkspace token="test-token" tenant={tenant} />);
  await userEvent.click(screen.getByRole("button", { name: "Load preview" }));
  await userEvent.clear(screen.getByLabelText("Application ID"));
  await userEvent.type(screen.getByLabelText("Application ID"), "app.other");
  resolve(response(fixture()));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Load preview" })).toBeEnabled(),
  );
  expect(
    screen.queryByRole("heading", { name: "Contact editor" }),
  ).not.toBeInTheDocument();
});
it("requires explicit reload after a stale update and carries the selected row version", async () => {
  const row = {
    id: tenant,
    version: 7,
    values: { "field.contact.id": tenant, "field.contact.name": "Old" },
  };
  const transport = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (url) =>
      String(url).endsWith("/commands")
        ? response({}, 409)
        : response(
            String(url).endsWith("/query")
              ? { records: [row], next: "" }
              : fixture(),
          ),
    );
  render(<ApplicationWorkspace token="test-token" tenant={tenant} />);
  await load();
  await userEvent.click(screen.getByRole("button", { name: "Open record" }));
  await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This record changed",
  );
  const calls = transport.mock.calls.filter(([url]) =>
    String(url).endsWith("/commands"),
  );
  expect(calls).toHaveLength(1);
  expect(JSON.parse(String(calls[0][1]?.body))).toMatchObject({
    operation: "update",
    id: tenant,
    expected_version: 7,
  });
});
it("blocks uncertain writes until a successful explicit reload and redacts transport messages", async () => {
  let commands = 0;
  let queryFailure = false;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url) => {
    if (String(url).endsWith("/commands")) {
      commands++;
      throw new TypeError("private network details");
    }
    if (String(url).endsWith("/query")) {
      if (queryFailure) throw new TypeError("private network details");
      return response({ records: [], next: "" });
    }
    return response(fixture());
  });
  render(<ApplicationWorkspace token="test-token" tenant={tenant} />);
  await load();
  await userEvent.type(screen.getByLabelText("Name"), "Possibly committed");
  await userEvent.click(screen.getByRole("button", { name: "Create record" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "may have committed",
  );
  expect(screen.getByRole("button", { name: "Create record" })).toBeDisabled();
  expect(screen.queryByText("private network details")).not.toBeInTheDocument();
  queryFailure = true;
  await userEvent.click(screen.getByRole("button", { name: "Reload records" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Reload records" }),
    ).toBeEnabled(),
  );
  expect(screen.getByRole("button", { name: "Create record" })).toBeDisabled();
  queryFailure = false;
  await userEvent.click(screen.getByRole("button", { name: "Reload records" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Create record" })).toBeEnabled(),
  );
  expect(commands).toBe(1);
});
it("supports compiler-valid entities without an exposed ID alias and rejects nested forms", () => {
  const p = preview();
  p.ir.entities[0].fields = p.ir.entities[0].fields.filter((f) => !f.core_id);
  expect(
    decodePreview(p, "app.contacts", 1).ir.entities[0].fields.some(
      (f) => f.core_id,
    ),
  ).toBe(false);
  p.ir.application.definitions[0].page.components = [
    {
      type: "form",
      children: [{ type: "section", children: [{ type: "form" }] }],
    },
  ];
  expect(() => decodePreview(p, "app.contacts", 1)).toThrow("Nested forms");
});
