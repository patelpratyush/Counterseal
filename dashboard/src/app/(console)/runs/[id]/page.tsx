import Link from "next/link";
import { createHash } from "node:crypto";
import { ArrowLeft } from "lucide-react";
import { control, runID } from "@/lib/api";
import type { Chain } from "@/lib/types";
import { RunDetail } from "@/components/run-detail";
import { Refresh } from "@/components/refresh";
import { RefundApprovals, type Approval } from "@/components/refund-approvals";
import { requireSession } from "@/lib/auth";
export default async function RunPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const [chain, history, { operator }] = await Promise.all([
    control<Chain>(`/v1/runs/${runID(id)}/chain`),
    control<{ approvals: Approval[]; as_of: string }>(`/v1/runs/${runID(id)}/approvals`), requireSession(),
  ]);
  const { approvals } = history;
  const now = Date.parse(history.as_of);
  return (
    <>
      <Link href="/" className="back-link">
        <ArrowLeft size={14} /> All runs
      </Link>
      <section className="page-heading run-heading">
        <div>
          <span className="eyebrow">RUN EXPLORER</span>
          <h1>
            {(chain.envelopes[0]?.envelope.purpose || "Workflow").replaceAll(
              "_",
              " ",
            )}
          </h1>
          <p className="mono">{id}</p>
        </div>
        <Refresh />
      </section>
      <RunDetail
        key={createHash("sha256").update(JSON.stringify([chain, approvals])).digest("hex")}
        chain={chain}
      />
      <RefundApprovals run={id} operator={operator} approvals={approvals} now={now} envelopes={chain.envelopes
        .filter(item => !item.revoked_at && new Date(item.envelope.expires_at).getTime() > now
          && item.envelope.allowed_actions.includes("refunds.create") && !item.envelope.denied_actions.includes("refunds.create"))
        .map(item => item.envelope)} />
    </>
  );
}
