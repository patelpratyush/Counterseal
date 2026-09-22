package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"handoffguard/internal/telemetry"
)

func TestMetricsAndTraceReflectCommittedDecisions(t *testing.T) {
	f := setup(t)
	id, run := f.root(t)
	f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 403)
	f.must(t, "POST", "/v1/approvals", approvalBody(id), 201)
	body, _ := json.Marshal(actionBody(id))
	req, _ := http.NewRequest("POST", f.http.URL+"/v1/evaluate/action", bytes.NewReader(body))
	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	req.Header.Set("traceparent", parent)
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	span, ok := telemetry.Parse(res.Header.Get("traceparent"))
	if !ok || span.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || span.SpanID == "00f067aa0ba902b7" {
		t.Fatal("trace not continued")
	}
	var audited bool
	err = f.db.Pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM audit_events WHERE run_id=$1 AND payload->>'trace_id'=$2 AND payload->>'span_id'=$3)", run, span.TraceID, span.SpanID).Scan(&audited)
	if err != nil || !audited {
		t.Fatalf("missing trace in audit: %v", err)
	}
	f.must(t, "POST", "/v1/evaluate/action", map[string]any{"bad": "PRIVATE_SENTINEL"}, 400)
	unauth, _ := http.Post(f.http.URL+"/v1/evaluate/action", "application/json", strings.NewReader("{}"))
	unauth.Body.Close()
	if unauth.StatusCode != 401 {
		t.Fatal(unauth.StatusCode)
	}
	get := func(token string) (int, string) {
		r, _ := http.NewRequest("GET", f.http.URL+"/metrics", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	if status, _ := get("wrong"); status != 401 {
		t.Fatal("public metrics")
	}
	status, metrics := get(testToken)
	if status != 200 {
		t.Fatal(status)
	}
	for _, want := range []string{`counterseal_authorization_requests_total{outcome="allow"} 1`, `counterseal_authorization_requests_total{outcome="deny"} 1`, `counterseal_authorization_requests_total{outcome="rejected"} 2`, `counterseal_authorization_duration_seconds_count 4`, `counterseal_authorization_duration_seconds_bucket{le="+Inf"} 4`} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("missing %s in %s", want, metrics)
		}
	}
	for _, secret := range []string{testToken, "PRIVATE_SENTINEL", id, run, "48319", span.TraceID} {
		if strings.Contains(metrics, secret) {
			t.Fatal("sensitive or unbounded metric label")
		}
	}
	// A transaction failure is an error, never a policy denial. Scraping works without DB access.
	f.db.Pool.Close()
	f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 500)
	_, metrics = get(testToken)
	if !strings.Contains(metrics, `counterseal_authorization_requests_total{outcome="error"} 1`) {
		t.Fatal(metrics)
	}
}

func TestMetricObservationsAreConcurrentAndCumulative(t *testing.T) {
	var m metrics
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { m.observe(200, true, "allow", .02) })
	}
	wg.Wait()
	if m.count != 100 || m.authorizations[0] != 100 || m.requests[2] != 100 {
		t.Fatal(m.count)
	}
	for i, bound := range latencyBounds {
		want := uint64(0)
		if bound >= .02 {
			want = 100
		}
		if m.buckets[i] != want {
			t.Fatal("non-cumulative histogram")
		}
	}
}
