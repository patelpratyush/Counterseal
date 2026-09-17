"use client";
import { useState, useTransition } from "react";
import dynamic from "next/dynamic";
import {
  ShieldCheck,
  ShieldAlert,
  GitBranch,
  ArrowRight,
  Check,
  Clock3,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { verifyAudit } from "@/app/actions";
import { date, type Chain, type Audit, type Envelope } from "@/lib/types";
const RunGraph = dynamic(() => import("./run-graph"), {
  ssr: false,
  loading: () => <div className="graph-loading">Loading delegation graph…</div>,
});
const fields = [
  "allowed_actions",
  "denied_actions",
  "resources",
  "data_classes",
  "approvals",
  "delegation",
  "expires_at",
] as const;
function pretty(value: unknown) {
  return typeof value === "string" ? value : JSON.stringify(value, null, 2);
}
function Constraints({ envelope }: { envelope: Envelope }) {
  return (
    <div className="constraints">
      <div>
        <label>Allowed actions</label>
        <div className="pills">
          {envelope.allowed_actions.map((a) => (
            <span key={a}>{a}</span>
          ))}
        </div>
      </div>
      <div>
        <label>Resource scope</label>
        {Object.entries(envelope.resources).map(([k, v]) => (
          <p key={k}>
            <strong>{k}</strong> <code>{v.join(", ")}</code>
          </p>
        ))}
      </div>
      <div>
        <label>Mandatory approvals</label>
        {envelope.approvals.length ? (
          envelope.approvals.map((a, i) => (
            <div className="approval" key={i}>
              <code>{a.condition}</code>
              <span>Requires {a.required_role}</span>
            </div>
          ))
        ) : (
          <p>No approval conditions.</p>
        )}
      </div>
      <div>
        <label>Denied actions</label>
        <p>{envelope.denied_actions.join(", ") || "None specified"}</p>
      </div>
      <div>
        <label>Data classes</label>
        <p>{envelope.data_classes.join(", ") || "None"}</p>
      </div>
      <div className="constraint-pair">
        <div>
          <label>Depth</label>
          <p>
            {envelope.delegation.current_depth} /{" "}
            {envelope.delegation.max_depth}
          </p>
        </div>
        <div>
          <label>Expires</label>
          <p>{date(envelope.expires_at)}</p>
        </div>
      </div>
    </div>
  );
}
export function RunDetail({ chain }: { chain: Chain }) {
  const [selected, setSelected] = useState(
    chain.envelopes[0]?.envelope.id || "",
  );
  const [audit, setAudit] = useState<Audit | null>(null);
  const [auditError, setAuditError] = useState("");
  const [pending, start] = useTransition();
  const stored = chain.envelopes.find((e) => e.envelope.id === selected);
  const handoff = chain.handoffs.find((h) => h.id === selected);
  const child = handoff
    ? chain.envelopes.find((e) => e.envelope.id === handoff.child_envelope_id)
        ?.envelope
    : undefined;
  const parent = handoff
    ? chain.envelopes.find((e) => e.envelope.id === handoff.parent_envelope_id)
        ?.envelope
    : undefined;
  const changed =
    parent && child
      ? fields.filter(
          (f) => JSON.stringify(parent[f]) !== JSON.stringify(child[f]),
        )
      : [];
  const events = [
    ...chain.handoffs.map((h) => ({
      id: h.id,
      type: "Handoff",
      title: `Delegation ${h.result.decision === "ALLOW" ? "allowed" : "denied"}`,
      time: h.created_at,
      result: h.result,
    })),
    ...chain.actions.map((a) => ({
      id: a.id,
      type: "Tool authorization",
      title: a.tool,
      time: a.created_at,
      result: a.result,
    })),
  ].sort((a, b) => b.time.localeCompare(a.time) || a.id.localeCompare(b.id));
  return (
    <>
      <div className="detail-summary">
        <span>
          <GitBranch size={15} />
          {chain.envelopes.length} signed envelopes
        </span>
        <span>{chain.handoffs.length} handoffs</span>
        <span>{chain.actions.length} tool decisions</span>
        <span className="detail-policy">
          {chain.envelopes[0]?.envelope.policy_version}
        </span>
      </div>
      <Tabs defaultValue="graph">
        <div className="detail-toolbar">
          <TabsList>
            <TabsTrigger value="graph">Delegation graph</TabsTrigger>
            <TabsTrigger value="activity">
              Decision history{" "}
              <span className="tab-count">{events.length}</span>
            </TabsTrigger>
          </TabsList>
          <Button
            variant="outline"
            disabled={pending}
            onClick={() => {
              setAuditError("");
              start(async () => {
                try {
                  setAudit(await verifyAudit(chain.run_id));
                } catch {
                  setAudit(null);
                  setAuditError(
                    "Audit verification failed. Refresh or sign in again.",
                  );
                }
              });
            }}
          >
            <ShieldCheck size={15} />
            {pending ? "Verifying…" : "Verify audit"}
          </Button>
        </div>
        <div aria-live="polite">
          {auditError ? (
            <div className="audit-banner invalid">{auditError}</div>
          ) : audit ? (
            <div
              className={`audit-banner ${audit.status === "INVALID" ? "invalid" : ""}`}
            >
              <ShieldCheck size={18} />
              <div>
                <strong>Audit {audit.status.toLowerCase()}</strong>
                <span>
                  {audit.events_checked} events · {audit.envelopes_checked}{" "}
                  envelope signatures checked
                </span>
                {audit.failures.map((f) => (
                  <p key={f}>{f}</p>
                ))}
              </div>
              <code title={audit.head_hash}>
                Head {audit.head_hash.slice(0, 16)}…
              </code>
            </div>
          ) : null}
        </div>
        <TabsContent value="graph">
          <div className="graph-workspace">
            <section className="graph-panel">
              <div className="graph-caption">
                <span>
                  <span className="tiny-dot" /> Authority map
                </span>
                <small>Click an agent or connection to inspect</small>
              </div>
              <RunGraph
                chain={chain}
                selected={selected}
                onSelect={setSelected}
              />
              <div className="graph-legend">
                <span>
                  <i /> Allowed delegation
                </span>
                <span>
                  <i className="denied-line" /> Denied attempt
                </span>
                <span>Scroll to zoom · Drag to pan</span>
              </div>
            </section>
            <aside className="inspector" aria-label="Selection inspector">
              <div className="inspector-header">
                <span className="eyebrow">
                  {handoff ? "HANDOFF INSPECTOR" : "ENVELOPE INSPECTOR"}
                </span>
                {handoff ? (
                  <>
                    <h2>
                      {handoff.result.decision === "ALLOW"
                        ? "Authority inherited"
                        : "Delegation blocked"}
                    </h2>
                    <Badge
                      className={
                        handoff.result.decision === "ALLOW"
                          ? "status-allow"
                          : "status-deny"
                      }
                    >
                      {handoff.result.decision}
                    </Badge>
                  </>
                ) : stored ? (
                  <>
                    <h2>{stored.envelope.recipient.agent}</h2>
                    <code>{stored.envelope.id}</code>
                    {stored.revoked_at ? (
                      <Badge className="status-deny">Revoked</Badge>
                    ) : null}
                  </>
                ) : (
                  <h2>Select an agent</h2>
                )}
              </div>
              <div className="inspector-content">
                {handoff ? (
                  <>
                    <div className="handoff-parties">
                      <span>
                        {parent?.recipient.agent || handoff.parent_envelope_id}
                      </span>
                      <ArrowRight size={16} />
                      <span>{child?.recipient.agent || "Proposed child"}</span>
                    </div>
                    {handoff.result.violations?.length ? (
                      <div className="violations">
                        {handoff.result.violations.map((v, i) => (
                          <div key={i}>
                            <strong>
                              <ShieldAlert size={14} />
                              {v.code}
                            </strong>
                            <p>{v.message}</p>
                            <code>{v.field}</code>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="inherit-message">
                        <Check size={15} /> Delegation passed policy evaluation.
                      </p>
                    )}
                    {parent && child ? (
                      <>
                        <div className="diff-summary">
                          <span>
                            <strong>{fields.length - changed.length}</strong>{" "}
                            unchanged fields
                          </span>
                          <span>
                            <strong>{changed.length}</strong> changed fields
                          </span>
                        </div>
                        <p className="muted-note">
                          Field comparison below. The ALLOW decision comes from
                          the policy engine.
                        </p>
                        {fields.map((f) => (
                          <details className="diff-field" key={f}>
                            <summary>
                              {f.replaceAll("_", " ")}
                              <span>
                                {changed.includes(f) ? "Changed" : "Retained"}
                              </span>
                            </summary>
                            {changed.includes(f) ? (
                              <>
                                <label>Parent</label>
                                <pre>{pretty(parent[f])}</pre>
                                <label>Child</label>
                                <pre>{pretty(child[f])}</pre>
                              </>
                            ) : (
                              <pre>{pretty(child[f])}</pre>
                            )}
                          </details>
                        ))}
                      </>
                    ) : (
                      <p className="muted-note">
                        No child envelope was issued for this attempt.
                      </p>
                    )}
                  </>
                ) : stored ? (
                  <>
                    <div className="envelope-context">
                      <label>Purpose</label>
                      <p>{stored.envelope.purpose}</p>
                      <label>Issued by</label>
                      <p>{stored.envelope.issuer.agent}</p>
                    </div>
                    <Constraints envelope={stored.envelope} />
                  </>
                ) : null}
                <div className="signature-note">
                  <ShieldCheck size={16} />
                  <span>
                    {audit?.status === "VALID"
                      ? "All persisted envelope signatures verified."
                      : "Use Verify audit to check envelope signatures and audit integrity."}
                  </span>
                </div>
              </div>
            </aside>
          </div>
          <div className="accessible-chain">
            <span className="eyebrow">QUICK INSPECT</span>
            {chain.envelopes.map(({ envelope: e }) => (
              <button
                key={e.id}
                onClick={() => setSelected(e.id)}
                aria-pressed={selected === e.id}
              >
                {e.recipient.agent}
              </button>
            ))}
            {chain.handoffs.map((h, i) => (
              <button
                key={h.id}
                onClick={() => setSelected(h.id)}
                aria-pressed={selected === h.id}
              >
                Handoff {i + 1} · {h.result.decision}
              </button>
            ))}
          </div>
        </TabsContent>
        <TabsContent value="activity">
          <section className="activity-panel">
            <div className="section-heading">
              <div>
                <h2>Decision history</h2>
                <p>
                  Authorization records. An allowed tool call does not confirm
                  upstream execution.
                </p>
              </div>
              <Clock3 size={20} />
            </div>
            {events.length ? (
              events.map((event) => (
                <article className="activity-row" key={event.id}>
                  <span
                    className={`event-dot ${event.result.decision === "DENY" ? "denied" : ""}`}
                  />
                  <div>
                    <small>{event.type}</small>
                    <h3>{event.title}</h3>
                    <code>{event.id}</code>
                    {event.result.violations?.map((v, i) => (
                      <p className="danger-text" key={i}>
                        {v.code}: {v.message}
                      </p>
                    ))}
                  </div>
                  <Badge
                    className={
                      event.result.decision === "ALLOW"
                        ? "status-allow"
                        : "status-deny"
                    }
                  >
                    {event.result.decision}
                  </Badge>
                  <time>{date(event.time)}</time>
                </article>
              ))
            ) : (
              <div className="empty-state">
                No authorization decisions recorded yet.
              </div>
            )}
          </section>
        </TabsContent>
      </Tabs>
    </>
  );
}
