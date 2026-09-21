CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS server_metadata (name text PRIMARY KEY, value text NOT NULL);
CREATE TABLE IF NOT EXISTS agents (name text NOT NULL, version text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(name,version));
CREATE TABLE IF NOT EXISTS runs (id text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS envelopes (
 id text PRIMARY KEY, run_id text NOT NULL REFERENCES runs(id), parent_id text REFERENCES envelopes(id),
 document jsonb NOT NULL, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS one_root_per_run ON envelopes(run_id) WHERE parent_id IS NULL;
CREATE INDEX IF NOT EXISTS envelopes_run ON envelopes(run_id);
CREATE INDEX IF NOT EXISTS envelopes_parent ON envelopes(parent_id);
CREATE TABLE IF NOT EXISTS handoffs (
 id text PRIMARY KEY, run_id text NOT NULL REFERENCES runs(id), parent_envelope_id text NOT NULL REFERENCES envelopes(id),
 child_envelope_id text REFERENCES envelopes(id), proposed_child_id text NOT NULL,
 decision text NOT NULL CHECK(decision IN ('ALLOW','DENY')), result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS approvals (
 id text PRIMARY KEY, envelope_id text NOT NULL REFERENCES envelopes(id), action text NOT NULL, resource text NOT NULL,
 arguments_hash text NOT NULL, approver_id text NOT NULL, approver_role text NOT NULL,
 expires_at timestamptz NOT NULL, consumed_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS approvals_lookup ON approvals(envelope_id,action,resource,arguments_hash);
CREATE TABLE IF NOT EXISTS tool_actions (
 id text PRIMARY KEY, run_id text NOT NULL REFERENCES runs(id), envelope_id text NOT NULL REFERENCES envelopes(id),
 agent_id text NOT NULL, tool_name text NOT NULL, arguments_hash text NOT NULL,
 decision text NOT NULL CHECK(decision IN ('ALLOW','DENY')), result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS violations (
 id bigserial PRIMARY KEY, run_id text NOT NULL REFERENCES runs(id), envelope_id text NOT NULL REFERENCES envelopes(id),
 rule_id text NOT NULL, field text NOT NULL, description text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS audit_events (
 seq bigserial PRIMARY KEY, id text UNIQUE NOT NULL, run_id text NOT NULL REFERENCES runs(id),
 event_type text NOT NULL, payload jsonb NOT NULL, payload_hash text NOT NULL, previous_event_hash text NOT NULL,
 event_hash text NOT NULL, timestamp timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_run_seq ON audit_events(run_id,seq);
INSERT INTO schema_migrations(version) VALUES(1) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS operators (
 id text PRIMARY KEY, username text UNIQUE NOT NULL, display_name text NOT NULL,
 role text NOT NULL CHECK(role IN ('viewer','refund_manager')), password_hash text NOT NULL,
 disabled boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS operator_sessions (
 token_hash text PRIMARY KEY, operator_id text NOT NULL REFERENCES operators(id),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS operator_sessions_owner ON operator_sessions(operator_id);
CREATE TABLE IF NOT EXISTS login_limits (
 bucket text PRIMARY KEY, attempts integer NOT NULL, reset_at timestamptz NOT NULL
);
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS operator_id text REFERENCES operators(id);
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS refund_amount bigint;
INSERT INTO schema_migrations(version) VALUES(2) ON CONFLICT DO NOTHING;
