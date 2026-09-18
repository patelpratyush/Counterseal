import Link from "next/link";
import { createHash } from "node:crypto";
import { ArrowLeft } from "lucide-react";
import { control, runID } from "@/lib/api";
import type { Chain } from "@/lib/types";
import { RunDetail } from "@/components/run-detail";
import { Refresh } from "@/components/refresh";
export default async function RunPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const chain = await control<Chain>(`/v1/runs/${runID(id)}/chain`);
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
        key={createHash("sha256").update(JSON.stringify(chain)).digest("hex")}
        chain={chain}
      />
    </>
  );
}
