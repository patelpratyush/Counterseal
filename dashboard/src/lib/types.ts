export type Violation = { code: string; field: string; message: string };
export type Decision = { decision: "ALLOW" | "DENY"; violations: Violation[] };
export type Envelope = {
  id: string;
  issuer: { agent: string };
  recipient: { agent: string };
  purpose: string;
  policy_version: string;
  parent_envelope?: string;
  allowed_actions: string[];
  denied_actions: string[];
  resources: Record<string, string[]>;
  data_classes: string[];
  approvals: { condition: string; required_role: string }[];
  delegation: { max_depth: number; current_depth: number };
  expires_at: string;
  signature: string;
};
export type StoredEnvelope = { envelope: Envelope; revoked_at?: string | null };
export type Handoff = {
  id: string;
  parent_envelope_id: string;
  child_envelope_id: string | null;
  proposed_child_id: string;
  result: Decision;
  created_at: string;
};
export type Action = {
  id: string;
  envelope_id: string;
  agent_id: string;
  tool: string;
  arguments_hash: string;
  result: Decision;
  created_at: string;
};
export type Chain = {
  run_id: string;
  envelopes: StoredEnvelope[];
  handoffs: Handoff[];
  actions: Action[];
};
export type Audit = {
  status: "VALID" | "INVALID";
  events_checked: number;
  envelopes_checked: number;
  head_hash: string;
  failures: string[];
};
export type RunSummary = {
  id: string;
  created_at: string;
  purpose: string;
  agent: string;
  policy_version: string;
  envelopes: number;
  handoffs: number;
  blocked: number;
};
export type Overview = {
  stats: {
    runs: number;
    handoffs: number;
    blocked_handoffs: number;
    blocked_actions: number;
    policy_versions: number;
  };
  runs: RunSummary[];
  total: number;
  page: number;
  page_size: number;
};
export function date(value: string) {
  return (
    new Intl.DateTimeFormat("en-GB", {
      day: "2-digit",
      month: "short",
      hour: "2-digit",
      minute: "2-digit",
      timeZone: "UTC",
    }).format(new Date(value)) + " UTC"
  );
}
