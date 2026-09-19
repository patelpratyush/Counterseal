# Policy comparison benchmark

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

## What was measured

`internal/policy/policy_test.go:BenchmarkDiff` repeatedly calls `Engine.Diff`
on a valid parent/child pair. The fixture has two allowed actions, one denial,
one resource category, two data classes, and one CEL approval condition.
The child inherits these constraints, advances depth, and shortens expiration.
Engine and fixture construction are excluded by `b.ResetTimer()`.

This measures in-process policy comparison, including the work performed by
`Diff`; it excludes signature verification, HTTP, PostgreSQL, MCP transport,
Java orchestration, and dashboard rendering. It does not establish end-to-end
throughput, concurrent load capacity, or performance for large policies.

## Reproduce

From the repository root:

```bash
go test ./internal/policy -run '^$' -bench '^BenchmarkDiff$' -benchmem -count 5
```

[Raw output](assets/policy-benchmark.txt) is included for inspection. Run on the
same host and toolchain when comparing changes; timings vary with system load.
