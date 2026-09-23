# API concurrency and policy-scale benchmarks

Measured on 2026-09-23 at benchmark source commit
`cd4d69528c867542b79521b777d25af018fc0076`. Two complete batches, each containing
three repetitions, exercised **18,000 timed API requests**: 9,000 expected allows
and 9,000 expected resource denials. Every response matched the expected status,
decision, and violation type; every case had the expected committed action count
and a valid audit chain. No unexpected responses were observed.

**These are development-laptop measurements, not a production capacity claim.**
The first batch showed substantial timing variation; the complete matrix was
repeated, and both batches are retained. In the smallest sequential case, run
throughput ranged from 96.2 to 836.3 requests/second. That variation prevents a
reliable claim about the effect of policy size or a release-to-release speedup.

## Concurrent authorization over HTTP

Each row summarizes six runs of 500 requests. Throughput and p50/p95/p99 are
medians of the six corresponding per-run statistics, not a pooled distribution
or one representative run. Throughput ranges include all six runs. Percentiles
use nearest rank over individual request durations, including any failed request;
the benchmark fails if any outcome is unexpected.

| Resource IDs in envelope | Concurrent workers | Median req/s | Req/s range | Median p50 ms | Median p95 ms | Median p99 ms |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 1 | 449.6 | 96.2–836.3 | 3.10 | 7.74 | 13.35 |
| 10 | 8 | 521.0 | 146.5–800.2 | 18.87 | 30.13 | 75.61 |
| 10 | 32 | 707.2 | 482.2–854.0 | 41.46 | 65.19 | 87.65 |
| 1000 | 1 | 440.6 | 361.1–454.2 | 2.15 | 3.16 | 3.58 |
| 1000 | 8 | 466.9 | 453.2–497.0 | 16.01 | 21.36 | 34.95 |
| 1000 | 32 | 516.2 | 484.2–560.1 | 58.81 | 72.61 | 92.91 |

The client and real Go HTTP server share one process and communicate over loopback.
PostgreSQL is a separate local process reached through a Unix socket. Each case
creates a fresh isolated schema and signed root envelope, then performs ten
sequential warmup requests before measurement. The HTTP client reuses connections;
additional connections opened by concurrent workers are included in measured time.
The pool allows eight PostgreSQL connections; concurrency above eight also queues
at the pool. Setup, migration, warmup, final action-count/audit verification, and
schema cleanup are outside the timer. PostgreSQL uses the test launcher's normal
initdb defaults; durability settings are not disabled for the benchmark.

The root has two allowed actions, one denial, one data class, one CEL approval
condition, and either 10 or 1,000 exact resource IDs. Half of requests authorize
a 100-unit refund on the **last** permitted resource; the other half request an
out-of-scope resource and must return `RESOURCE_DENIED`. Each request commits
its action/audit records (and violation records for a denial). Requests include
service authentication, envelope/signature checks, resource and CEL evaluation,
PostgreSQL transaction waiting and commit, JSON transfer, and client-side response
validation. Human approval consumption, delegated ancestry, TLS, a remote network,
MCP, Java, and actual upstream refund execution are not measured.

Workers run **closed loop**: each starts its next request only after the previous
response completes. This is a fixed in-flight concurrency workload, not a fixed
arrival-rate overload test; it does not measure latency for requests waiting
outside those workers. Go's default `ns/op` here is total elapsed time divided
by request count, **not** per-request latency under concurrency. Use the custom
p50/p95/p99 columns for observed latency.

The service intentionally uses one global transaction advisory lock to order
approval consumption, revocation, and audit writes. Higher concurrency can build
queues rather than yield parallel transaction execution. The 1,000-resource cases
show modest throughput change but much higher latency with more workers, consistent
with that design. This is an inference from the workload and implementation, not
a bottleneck attribution established by CPU/lock profiling.

## Larger in-process policy comparisons

Each parent and child has N allowed actions **and** N exact resource IDs, plus
one denied action, two data classes, and one CEL approval condition. The allow
case preserves scope while advancing delegation depth and shortening expiration.
The deny case changes the last child resource to an unauthorized ID. Fixture and
engine construction are outside the timer; every result is checked. Each run
uses Go's adaptive 500 ms benchmark duration, and each statistic below is the
median across six run averages. B/op is rounded to the nearest byte.

| N actions + N resource IDs | Decision | Median ms/op | Median B/op | Median allocs/op |
| ---: | --- | ---: | ---: | ---: |
| 10 | allow | 0.385 | 51,916 | 981 |
| 10 | deny | 0.236 | 52,056 | 985 |
| 100 | allow | 0.514 | 99,201 | 1017 |
| 100 | deny | 0.528 | 99,343 | 1021 |
| 1000 | allow | 12.030 | 799,823 | 1085 |
| 1000 | deny | 12.064 | 799,984 | 1089 |

These are policy comparisons, not API latency percentiles. They exclude signing,
HTTP, database access, MCP and Java. Timings share the same host-variability caveat;
allocation growth is also shown to make the increase in work visible. The 1,000-entry
cases take substantially more work than the old small fixture; the old 0.150 ms
headline must not be applied to arbitrary policy sizes or complete API requests.

## Environment and reproduction

Go 1.26.5, PostgreSQL 18.6 (Homebrew), macOS 15.7.9, darwin/amd64,
Intel Core i7-8557U at 1.70 GHz, 8 GiB RAM, GOMAXPROCS 8.
The host was a shared development laptop, without CPU pinning or controlled power/
thermal/background-load conditions. Cases run in fixed order; no randomized ordering
or confidence intervals are provided. Five hundred samples per run are insufficient
for a strong rare-tail/SLO claim. Trace logging was off, metrics remained enabled,
and no race instrumentation was enabled for the published measurements.

Run from the repository root with Go and local PostgreSQL installed:

```bash
bash scripts/benchmark.sh full | tee /tmp/counterseal-benchmark.txt
# Short correctness exercise; do not use these timings as performance evidence.
bash scripts/benchmark.sh smoke
```

The script always creates and removes a disposable PostgreSQL instance; Docker is
not used. Each sub-benchmark uses an isolated schema. CI runs the short matrix and
fails on incorrect outcomes or audit/count mismatches, with **no timing threshold**
on shared runners. Race-instrumented smoke checks were run separately to verify
the concurrent harness; their timings are excluded here.

Raw outputs: [batch 1](assets/benchmark-2026-09-23-batch-1.txt),
[batch 2](assets/benchmark-2026-09-23-batch-2.txt). Both include source SHA, tool
versions, all run values, and PostgreSQL pool size. Keep workload, sample count,
host, toolchain and database settings fixed when comparing revisions. Dedicated-host
runs, randomized case order, open-loop load, longer durations, deeper delegation
chains and distributed deployments remain future performance work.

## Historical baseline — 2026-09-19

Measured on 2026-09-19 using the policy implementation at `6da1c20`.
Five runs of the existing `BenchmarkDiff` produced a median **149.732 µs/op**
(approximately **0.150 ms**), **48,357 B/op**, and **963 allocations/op**.
These are medians of Go benchmark run averages, not request-latency percentiles.

| Run | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| 1 | 166,453 | 48,359 | 963 |
| 2 | 152,045 | 48,358 | 963 |
| 3 | 149,732 | 48,357 | 963 |
| 4 | 149,438 | 48,356 | 963 |
| 5 | 147,501 | 48,354 | 963 |

Environment: Go 1.26.5, macOS 15.7.9, darwin/amd64, Intel Core i7-8557U
at 1.70 GHz, 8 GiB RAM. The benchmark suffix `-8` records GOMAXPROCS;
the benchmark loop itself is sequential. This was a development laptop,
not a dedicated performance host.

### What was measured

`internal/policy/policy_test.go:BenchmarkDiff` repeatedly calls `Engine.Diff`
on a valid parent/child pair. The fixture has two allowed actions, one denial,
one resource category, two data classes, and one CEL approval condition.
The child inherits these constraints, advances depth, and shortens expiration.
Engine and fixture construction are excluded by `b.ResetTimer()`.

This measures in-process policy comparison, including the work performed by
`Diff`; it excludes signature verification, HTTP, PostgreSQL, MCP transport,
Java orchestration, and dashboard rendering. It does not establish end-to-end
throughput, concurrent load capacity, or performance for large policies.

### Reproduce

From the repository root:

```bash
go test ./internal/policy -run '^$' -bench '^BenchmarkDiff$' -benchmem -count 5
```

[Raw output](assets/policy-benchmark.txt) is included for inspection. Run on the
same host and toolchain when comparing changes; timings vary with system load.
