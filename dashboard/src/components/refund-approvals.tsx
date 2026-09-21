"use client";

import { useActionState, useState } from "react";
import { ShieldCheck, UserCheck } from "lucide-react";
import { approveRefund } from "@/app/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { Envelope } from "@/lib/types";
import type { Operator } from "@/lib/auth";

export type Approval = {
  id: string; envelope_id: string; resource: string; username: string; display_name: string;
  operator_id: string | null; role: string; amount: number | null; created_at: string;
  expires_at: string; consumed_at: string | null;
};
export function RefundApprovals({ run, envelopes, operator, approvals, now }: {
  run: string; envelopes: Envelope[]; operator: Operator; approvals: Approval[]; now: number;
}) {
  const [selected, setSelected] = useState(envelopes.find(e => e.recipient.agent === "billing-agent")?.id || envelopes[0]?.id || "");
  const [state, action, pending] = useActionState(approveRefund, { error: "", success: "" });
  const envelope = envelopes.find(e => e.id === selected);
  const orders = envelope?.resources.orders?.filter(id => !/[\*?\[\]]/.test(id)) || [];
  return (
    <section className="approval-ledger" aria-labelledby="approval-heading">
      <header className="approval-heading">
        <div><span className="eyebrow">HUMAN AUTHORIZATION</span><h2 id="approval-heading">Refund approvals</h2>
          <p>One exact refund. One named decision-maker.</p></div>
        <ShieldCheck size={24} aria-hidden="true" />
      </header>
      <div className="approval-columns">
        <div className="approval-compose">
          {operator.role === "refund_manager" && envelope ? (
            <form action={action} aria-label="Approve refund" className="approval-form">
              <h3>Approve a prepared refund</h3>
              <p>Use the order and amount printed by the prepared Java workflow. Approval permits one attempt; it does not execute the refund.</p>
              <input type="hidden" name="run_id" value={run} />
              <label htmlFor="approval-envelope">Delegated envelope</label>
              <select id="approval-envelope" name="envelope_id" value={selected} onChange={e => setSelected(e.target.value)}>
                {envelopes.map(e => <option key={e.id} value={e.id}>{e.recipient.agent} · {e.id}</option>)}
              </select>
              <div className="approval-fields">
                <div><label htmlFor="approval-order">Order ID</label>
                  <select key={selected} id="approval-order" name="order_id" required defaultValue={orders[0]}>
                    {orders.map(order => <option key={order} value={order}>{order}</option>)}
                  </select></div>
                <div><label htmlFor="approval-amount">Refund amount</label>
                  <Input id="approval-amount" name="amount" type="number" min="1" max="9007199254740991" step="1" placeholder="825" required /></div>
              </div>
              <p className="approval-scope">Exact integer units used by the workflow. Valid for up to 15 minutes, within the envelope expiry.</p>
              <label className="approval-confirm"><input type="checkbox" name="confirm" required />
                <span>I confirm this exact order and amount.</span></label>
              <div className="approval-signer"><UserCheck size={16} /><span>Signing as <strong>{operator.display_name}</strong><small>{operator.username} · Refund manager</small></span></div>
              {state.error && <p role="alert" className="danger-text">{state.error}</p>}
              {state.success && <p role="status" className="approval-success">{state.success}</p>}
              <Button type="submit" disabled={pending || !orders.length}>{pending ? "Recording approval…" : "Approve exact refund"}</Button>
            </form>
          ) : <div className="approval-empty"><h3>{operator.role === "viewer" ? "View decisions, without approving" : "No eligible refund envelope"}</h3>
            <p>{operator.role === "viewer" ? "Only a refund manager can authorize a refund. Every decision is attributed to their individual account." : "Prepare a refund workflow to issue a Billing envelope, then return to this run."}</p></div>}
        </div>
        <div className="approval-history">
          <h3>Approval record <span>{approvals.length}</span></h3>
          {!approvals.length && <p className="approval-empty">No refunds approved for this run yet.</p>}
          <ol>{approvals.map(approval => <li key={approval.id}>
            <div className="approval-record-title"><strong>{approval.amount === null ? "Legacy approval" : `${approval.amount.toLocaleString("en-US")} units`}</strong>
              <span className="approval-status">{approval.consumed_at ? "Consumed" : new Date(approval.expires_at).getTime() <= now ? "Expired" : "Approved"}</span></div>
            <p>{approval.resource}</p>
            <p><strong>{approval.display_name}</strong> <span>({approval.username})</span></p>
            <small>{approval.operator_id ? "Verified operator" : "Legacy demo identity"} · {new Date(approval.created_at).toISOString().replace("T", " ").slice(0, 19)} UTC</small>
            <small className="mono">{approval.envelope_id}</small>
          </li>)}</ol>
          {approvals.length === 100 && <p>Showing the latest 100 approvals.</p>}
        </div>
      </div>
    </section>
  );
}
