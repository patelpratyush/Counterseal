package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"handoffguard/internal/policy"
)

// Fixed closed-loop workers against real loopback HTTP and PostgreSQL. Run with
// -benchtime=500x: a fixed sample count also fixes audit-table growth across cases.
func BenchmarkAuthorizationHTTP(b *testing.B) {
	for _, scopes := range []int{10, 1000} {
		for _, workers := range []int{1, 8, 32} {
			b.Run(fmt.Sprintf("resources=%d/workers=%d", scopes, workers), func(b *testing.B) {
				f := setup(b)
				env := draft()
				env.Resources["orders"] = make([]string, scopes)
				for i := range scopes {
					env.Resources["orders"][i] = fmt.Sprintf("order-%06d", i)
				}
				root := f.must(b, "POST", "/v1/envelopes", map[string]any{"envelope": env}, 201)
				id, run := root["envelope"].(map[string]any)["id"].(string), root["run_id"].(string)
				var payloads [2][]byte
				for i, order := range []string{env.Resources["orders"][scopes-1], "outside-policy"} {
					payloads[i], _ = json.Marshal(map[string]any{"envelope_id": id, "agent_id": "billing", "tool": "refunds.create",
						"resources": map[string][]string{"orders": {order}}, "data_classes": []string{"pii"}, "arguments": map[string]any{"order_id": order, "amount": 100}})
				}
				transport := &http.Transport{MaxIdleConns: workers, MaxIdleConnsPerHost: workers, MaxConnsPerHost: workers}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
				request := func(i int) error {
					req, err := http.NewRequest("POST", f.http.URL+"/v1/evaluate/action", bytes.NewReader(payloads[i%2]))
					if err != nil {
						return err
					}
					req.Header.Set("Authorization", "Bearer "+testToken)
					req.Header.Set("Content-Type", "application/json")
					res, err := client.Do(req)
					if err != nil {
						return fmt.Errorf("HTTP request failed")
					}
					defer res.Body.Close()
					var body struct {
						ActionID string        `json:"action_id"`
						Result   policy.Result `json:"result"`
					}
					if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
						return fmt.Errorf("invalid response JSON")
					}
					if _, err := io.Copy(io.Discard, res.Body); err != nil {
						return fmt.Errorf("incomplete response")
					}
					code, decision := 200, "ALLOW"
					if i%2 == 1 {
						code, decision = 403, "DENY"
					}
					if res.StatusCode != code || body.Result.Decision != decision || body.ActionID == "" {
						return fmt.Errorf("expected %d/%s, got %d/%s", code, decision, res.StatusCode, body.Result.Decision)
					}
					if (i%2 == 0 && len(body.Result.Violations) != 0) || (i%2 == 1 && (len(body.Result.Violations) != 1 || body.Result.Violations[0].Code != "RESOURCE_DENIED")) {
						return fmt.Errorf("unexpected policy violations")
					}
					return nil
				}
				const warmup = 10
				b.Logf("PostgreSQL pool MaxConns=%d; sequential warmup=%d; fresh schema per case", f.db.Pool.Config().MaxConns, warmup)
				for i := range warmup {
					if err := request(i); err != nil {
						b.Fatal(err)
					}
				}
				latencies := make([]time.Duration, b.N)
				var next, errors atomic.Int64
				firstError := make(chan error, 1)
				start := make(chan struct{})
				var ready, finished sync.WaitGroup
				for range workers {
					ready.Add(1)
					finished.Add(1)
					go func() {
						defer finished.Done()
						ready.Done()
						<-start
						for {
							i := int(next.Add(1) - 1)
							if i >= b.N {
								return
							}
							began := time.Now()
							err := request(i)
							latencies[i] = time.Since(began)
							if err != nil {
								errors.Add(1)
								select {
								case firstError <- err:
								default:
								}
							}
						}
					}()
				}
				ready.Wait()
				b.ResetTimer()
				began := time.Now()
				close(start)
				finished.Wait()
				elapsed := time.Since(began)
				b.StopTimer()
				b.ReportMetric(float64(b.N)/elapsed.Seconds(), "req/s")
				b.ReportMetric(float64(errors.Load()), "errors")
				b.ReportMetric(float64((b.N+1)/2), "expected-allow")
				b.ReportMetric(float64(b.N/2), "expected-deny")
				for _, p := range []int{50, 95, 99} {
					b.ReportMetric(float64(percentile(latencies, p))/float64(time.Microsecond), fmt.Sprintf("p%d-us", p))
				}
				if errors.Load() != 0 {
					b.Fatalf("%d failed requests; first: %v", errors.Load(), <-firstError)
				}
				var actions int
				if err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM tool_actions WHERE run_id=$1", run).Scan(&actions); err != nil || actions != b.N+warmup {
					b.Fatalf("committed action count: %d, want %d; error: %v", actions, b.N+warmup, err)
				}
				if audit := f.must(b, "POST", "/v1/audit/"+run+"/verify", nil, 200); audit["status"] != "VALID" {
					b.Fatal("invalid audit after load")
				}
			})
		}
	}
}

// Nearest-rank percentiles over individual request latency, including failures.
func percentile(samples []time.Duration, p int) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[int(math.Ceil(float64(p)*float64(len(samples))/100))-1]
}

func TestBenchmarkPercentile(t *testing.T) {
	if percentile([]time.Duration{4, 1, 3, 2}, 50) != 2 || percentile([]time.Duration{4, 1, 3, 2}, 99) != 4 || percentile(nil, 95) != 0 {
		t.Fatal("incorrect nearest-rank percentile")
	}
}
