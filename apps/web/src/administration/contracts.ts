import { object, uuid } from "../preview/contracts";

export interface Team { id: string; name: string; parent_id: string; version: number }
export interface TeamMember { user_id: string; active: boolean }
export interface TeamDetail extends Team { members: TeamMember[] }
export interface RecordAccess { record_id: string; version: number; owner_user_id: string; owner_team_id: string; shares: { user_id: string; can_write: boolean }[]; shares_truncated: boolean }
export const version = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) > 0 && Number(v) < Number.MAX_SAFE_INTEGER;
const optionalID = (v: unknown): v is string | undefined => v === undefined || v === "" || uuid(v);
function unique<T>(items: T[], id: (item: T) => string) { return new Set(items.map(id)).size === items.length; }
export function decodeTeam(v: unknown, id?: string): Team {
  if (!object(v) || !uuid(v.id) || id !== undefined && v.id !== id ||
    typeof v.name !== "string" || [...v.name].length < 1 || [...v.name].length > 120 ||
    v.name.trim() !== v.name || /\p{Cc}/u.test(v.name) ||
    !optionalID(v.parent_id) || !version(v.version)) throw new Error("Invalid team response");
  return { id: v.id, name: v.name, parent_id: v.parent_id ?? "", version: v.version };
}
export function decodeTeams(v: unknown): Team[] {
  if (!object(v) || !Array.isArray(v.teams) || v.teams.length > 256) throw new Error("Invalid teams response");
  const teams = v.teams.map((item) => decodeTeam(item));
  if (!unique(teams, (t) => t.id)) throw new Error("Duplicate team response");
  return teams;
}
export function decodeMember(v: unknown): TeamMember {
  if (!object(v) || !uuid(v.user_id) || typeof v.active !== "boolean") throw new Error("Invalid membership response");
  return { user_id: v.user_id, active: v.active };
}
export function decodeTeamDetail(v: unknown, id: string): TeamDetail {
  const team = decodeTeam(v, id);
  if (!object(v) || !Array.isArray(v.members) || v.members.length > 1000) throw new Error("Invalid team members response");
  const members = v.members.map(decodeMember);
  if (!unique(members, (m) => m.user_id)) throw new Error("Duplicate membership response");
  return { ...team, members };
}
export function decodeMembers(v: unknown): (TeamMember & { role: string })[] {
  if (!object(v) || !Array.isArray(v.members) || v.members.length > 100) throw new Error("Invalid members response");
  const members = v.members.map((v: unknown) => {
    const member = decodeMember(v);
    if (!object(v) || typeof v.role !== "string" || v.role.length > 80) throw new Error("Invalid member role");
    return { ...member, role: v.role };
  });
  if (!unique(members, (m) => m.user_id)) throw new Error("Duplicate member response");
  return members;
}
export function decodeAccess(v: unknown, id: string): RecordAccess {
  if (!object(v) || v.record_id !== id || !uuid(v.record_id) || !version(v.version) ||
    !uuid(v.owner_user_id) || !optionalID(v.owner_team_id) ||
    v.shares_truncated !== undefined && typeof v.shares_truncated !== "boolean" ||
    v.shares !== undefined && !Array.isArray(v.shares)) throw new Error("Invalid record access response");
  const shares: RecordAccess["shares"] = [];
  for (const share of (v.shares ?? []) as unknown[]) {
    if (!object(share) || !uuid(share.user_id) || typeof share.can_write !== "boolean") throw new Error("Invalid share response");
    shares.push({ user_id: share.user_id, can_write: share.can_write });
    if (shares.length > 256) throw new Error("Share response budget exceeded");
  }
  if (!unique(shares, (s) => s.user_id)) throw new Error("Duplicate share response");
  return { record_id: id, version: v.version, owner_user_id: v.owner_user_id, owner_team_id: v.owner_team_id ?? "", shares, shares_truncated: v.shares_truncated ?? false };
}
