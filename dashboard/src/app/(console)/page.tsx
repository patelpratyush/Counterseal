import Link from "next/link";
import {
  ArrowRight,
  GitBranch,
  ShieldBan,
  Search,
  Layers,
  Activity,
} from "lucide-react";
import { control } from "@/lib/api";
import type { Overview } from "@/lib/types";
import { date } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Refresh } from "@/components/refresh";
export default async function OverviewPage({
  searchParams,
}: {
  searchParams: Promise<{ q?: string; page?: string; filter?: string }>;
}) {
  const search = await searchParams;
  const q = typeof search.q === "string" ? search.q.slice(0, 100) : "";
  const page = Math.max(1, Math.min(100000, Number(search.page) || 1));
  const filter = search.filter === "blocked" ? "blocked" : "";
  const data = await control<Overview>(
    `/v1/dashboard/overview?${new URLSearchParams({ q, page: String(Math.floor(page)), filter })}`,
  );
  const url = (p: number, f = filter) =>
    `/?${new URLSearchParams({ q, page: String(p), filter: f })}`;
  const stats = [
    {
      label: "Total runs",
      value: data.stats.runs,
      icon: Activity,
      note: "Recorded workflows",
    },
    {
      label: "Agent handoffs",
      value: data.stats.handoffs,
      icon: GitBranch,
      note: "Evaluated delegations",
    },
    {
      label: "Blocked decisions",
      value: data.stats.blocked_handoffs + data.stats.blocked_actions,
      icon: ShieldBan,
      note: `${data.stats.blocked_handoffs} handoffs · ${data.stats.blocked_actions} tool calls`,
    },
    {
      label: "Policy versions",
      value: data.stats.policy_versions,
      icon: Layers,
      note: "Referenced by envelopes",
    },
  ];
  return (
    <>
      <section className="page-heading">
        <div>
          <span className="eyebrow">YOUR SYSTEM, IN VIEW</span>
          <h1>
            Authority overview<span className="heading-dot">.</span>
          </h1>
          <p>Follow the work. See where trust holds.</p>
        </div>
        <Refresh />
      </section>
      <section className="stats-grid" aria-label="Workspace statistics">
        {stats.map((stat, i) => (
          <article className={`stat-card stat-${i}`} key={stat.label}>
            <div className="stat-label">
              {stat.label}
              <stat.icon size={18} />
            </div>
            <strong>{stat.value.toLocaleString("en-US")}</strong>
            <small>{stat.note}</small>
          </article>
        ))}
      </section>
      <section className="runs-section">
        <div className="section-heading">
          <div>
            <h2>
              Workflow runs <span className="count">{data.total}</span>
            </h2>
            <p>Every delegation leaves a trail.</p>
          </div>
          <div className="filter-tabs">
            <Link href={url(1, "")} aria-current={!filter ? "page" : undefined}>
              All runs
            </Link>
            <Link
              href={url(1, "blocked")}
              aria-current={filter ? "page" : undefined}
            >
              With blocks
            </Link>
          </div>
        </div>
        <form className="search-row">
          <Search size={17} />
          <Input
            aria-label="Search runs"
            name="q"
            defaultValue={q}
            placeholder="Search by run ID or purpose…"
            maxLength={100}
          />
          <input type="hidden" name="filter" value={filter} />
          <Button variant="outline" type="submit">
            Search
          </Button>
        </form>
        <div className="table-scroll">
          <table className="runs-table">
            <thead>
              <tr>
                <th>Run / purpose</th>
                <th>Entry agent</th>
                <th>Delegation</th>
                <th>Decisions</th>
                <th>Created</th>
                <th>
                  <span className="sr-only">Open</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {data.runs.map((run) => (
                <tr key={run.id}>
                  <td>
                    <Link className="run-link" href={`/runs/${run.id}`}>
                      {run.purpose.replaceAll("_", " ")}
                      <span>{run.id}</span>
                    </Link>
                  </td>
                  <td>
                    <span className="agent-label">
                      <span className="tiny-dot" />
                      {run.agent}
                    </span>
                  </td>
                  <td>
                    {run.envelopes} envelopes
                    <small>{run.handoffs} handoffs</small>
                  </td>
                  <td>
                    <Badge
                      className={run.blocked ? "status-deny" : "status-neutral"}
                    >
                      {run.blocked
                        ? `${run.blocked} blocked`
                        : "No blocks recorded"}
                    </Badge>
                  </td>
                  <td className="date-cell">{date(run.created_at)}</td>
                  <td>
                    <Link
                      className="row-arrow"
                      href={`/runs/${run.id}`}
                      aria-label={`Inspect ${run.id}`}
                    >
                      <ArrowRight size={17} />
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!data.runs.length ? (
          <div className="empty-state">
            <GitBranch size={32} />
            <h3>
              {q || filter
                ? "No matching runs"
                : "Your first run starts the story."}
            </h3>
            <p>
              {q || filter
                ? "Try another search or show all runs."
                : "Run the agent demo to see signed handoffs and tool decisions here."}
            </p>
            <Link href="/">Show all runs →</Link>
          </div>
        ) : null}
        <div className="table-footer">
          <span>
            Showing {data.runs.length} of {data.total} runs
          </span>
          <div>
            {data.page > 1 ? (
              <Link href={url(data.page - 1)}>← Previous</Link>
            ) : (
              <span>← Previous</span>
            )}
            <span>Page {data.page}</span>
            {data.page * data.page_size < data.total ? (
              <Link href={url(data.page + 1)}>Next →</Link>
            ) : (
              <span>Next →</span>
            )}
          </div>
        </div>
      </section>
      <section className="overview-note">
        <span className="note-symbol">↳</span>
        <p>
          <strong>Permissions travel with the work.</strong> Open a run to
          inspect its signed envelopes, inherited constraints, and authorization
          decisions.
        </p>
        <span className="note-tag">MONOTONIC BY DESIGN</span>
      </section>
    </>
  );
}
