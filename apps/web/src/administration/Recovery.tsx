import { createContext, useContext, useEffect, useState, type ReactNode, type Dispatch, type SetStateAction } from "react";

export interface AdministrationIntent {
  status?: "dispatched" | "unknown";
  scope: string;
  kind: "team.create" | "team.save" | "team.member" | "record.owner" | "record.share" | "record.unshare" | "role.publish" | "role.assign";
  tenant: string;
  resource: string;
  target?: string;
  expectedVersion?: number;
  operationID?: string;
  expectedHash?: string;
  expectedActive?: boolean;
  expectedRole?: string;
}
interface Recovery {
  commandJournal: Record<string, string>;
  setCommandJournal: Dispatch<SetStateAction<Record<string, string>>>;
  inFlight: Record<string, string>;
  setInFlight: Dispatch<SetStateAction<Record<string, string>>>;
  administrationJournal: Record<string, AdministrationIntent>;
  setAdministrationJournal: Dispatch<SetStateAction<Record<string, AdministrationIntent>>>;
}
export const RecoveryContext = createContext<Recovery | undefined>(undefined);
export function RecoveryProvider({ children }: { children: ReactNode }) {
  const [commandJournal, setCommandJournal] = useState<Record<string, string>>({});
  const [inFlight, setInFlight] = useState<Record<string, string>>({});
  const [administrationJournal, setAdministrationJournal] = useState<Record<string, AdministrationIntent>>({});
  useEffect(() => {
    if (!Object.keys(commandJournal).length && !Object.keys(administrationJournal).length) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [commandJournal, administrationJournal]);
  return <RecoveryContext.Provider value={{ commandJournal, setCommandJournal, inFlight, setInFlight, administrationJournal, setAdministrationJournal }}>{children}</RecoveryContext.Provider>;
}
export function useRecovery() {
  const value = useContext(RecoveryContext);
  if (!value) throw new Error("Recovery provider missing");
  return value;
}
export interface AdministrationPort {
  pending: Record<string, AdministrationIntent>;
  remember: (scope: string, intent?: AdministrationIntent) => void;
  busy: (key: string, pending: boolean) => void;
}
