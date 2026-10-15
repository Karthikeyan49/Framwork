import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import source from "../../../contracts/examples/contacts-draft-v1.json";
import { AccessConsole } from "./AccessConsole";

const tenant = "11111111-1111-1111-1111-111111111111";
const otherTenant = "33333333-3333-3333-3333-333333333333";
const owner = "22222222-2222-2222-2222-222222222222";
const otherOwner = "44444444-4444-4444-4444-444444444444";
const response = (value: unknown) => new Response(JSON.stringify(value));
function descriptor() {
  const doc = structuredClone(source);
  return {
    mode: "preview", revision: 1, ir_hash: "a".repeat(64), can_write: true,
    ir: {
      ir_version: "1.0.0", compiler_version: "1.0.0",
      schema_snapshot: "hybrid-preview-v1", application: doc,
      entities: doc.definitions.filter((d) => d.entity).map((d) => ({
        id: d.id, label: d.label,
        fields: doc.definitions.filter((f) => f.field?.entity_ref === d.id).map((f) => ({
          id: f.id, label: f.label, ...f.field, core_id: f.id.endsWith(".id"),
        })),
      })),
    },
  };
}
async function connect() {
  await userEvent.type(screen.getByLabelText("API access token"), "synthetic-token");
  await userEvent.click(screen.getByRole("button", { name: "Connect" }));
  await screen.findByRole("heading", { name: "Owner" });
}
async function openWorkspace() {
  await userEvent.selectOptions(screen.getByLabelText("Workspace"), tenant);
  await screen.findByText("Access verified · owner");
  await userEvent.click(screen.getByRole("button", { name: "Load preview" }));
  await screen.findByRole("heading", { name: "Contact editor" });
  await waitFor(() => expect(screen.getByRole("button", { name: "Reload records" })).toBeEnabled());
}

it("retains a dispatched key through disconnect and account/workspace switches until owned reconciliation and a fresh read", async () => {
  let actor = owner;
  let key = "";
  let writes = 0;
  let committed = false;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url, options) => {
    const path = String(url);
    if (path.endsWith("/session")) return response({
      user: { id: actor, name: "Owner" },
      tenants: [{ id: tenant, name: "Workspace A" }, { id: otherTenant, name: "Workspace B" }],
    });
    if (path.endsWith("/access")) return response({
      tenant_id: path.includes(otherTenant) ? otherTenant : tenant,
      roles: ["owner"], decision: { allow: true, decision_id: "decision", policy_version: "m1-native-v1" },
    });
    if (path.endsWith("/query")) return response({ records: [], next: "" });
    if (path.endsWith("/commands")) {
      writes++;
      key = JSON.parse(String(options?.body)).command_key;
      return new Promise<Response>((_resolve, reject) => {
        options?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
      });
    }
    if (path.endsWith("/commands/" + key)) return response({
      command_key: key, state: committed ? "COMMITTED" : "UNKNOWN",
      ...(committed ? { record_id: otherTenant, record_version: 1 } : {}),
    });
    return response(descriptor());
  });
  render(<AccessConsole />);
  await connect();
  await openWorkspace();
  await userEvent.type(screen.getByLabelText("Name"), "One intent");
  await userEvent.click(screen.getByRole("button", { name: "Create record" }));
  await waitFor(() => expect(writes).toBe(1));
  expect(screen.getAllByText(key).length).toBeGreaterThan(0);
  expect(screen.getByLabelText("Workspace")).toBeDisabled();

  // Disconnect clears credentials, but cannot prove the dispatched write failed.
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  expect(screen.queryByText(key)).not.toBeInTheDocument();
  actor = otherOwner;
  await connect();
  expect(screen.queryByText(key)).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Disconnect" }));

  actor = owner;
  await connect();
  expect(screen.getByText(key)).toBeInTheDocument();
  await openWorkspace();
  expect(screen.getByRole("button", { name: "Create record" })).toBeDisabled();
  await userEvent.selectOptions(screen.getByLabelText("Workspace"), otherTenant);
  await screen.findByText("Access verified · owner");
  await openWorkspace();
  expect(screen.getByRole("button", { name: "Create record" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "Check command receipt" }));
  await screen.findByText(/receipt is still unknown/);
  await userEvent.click(screen.getByRole("button", { name: "Reload records" }));
  expect(screen.getByRole("button", { name: "Create record" })).toBeDisabled();
  committed = true;
  await userEvent.click(screen.getByRole("button", { name: "Check command receipt" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Create record" })).toBeEnabled());
  expect(screen.queryByText(key)).not.toBeInTheDocument();
  expect(writes).toBe(1);
});
