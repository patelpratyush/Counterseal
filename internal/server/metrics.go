package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// Fixed labels only. No run IDs, agents, resources, credentials, or input paths.
type metrics struct {
	mu             sync.Mutex
	requests       [6]uint64
	authorizations [4]uint64
	buckets        [9]uint64
	sum            float64
	count          uint64
}

var latencyBounds = [...]float64{.001, .005, .01, .025, .05, .1, .25, 1, 5}
var authorizationOutcomes = [...]string{"allow", "deny", "rejected", "error"}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (m *metrics) observe(status int, action bool, outcome string, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	class := status / 100
	if class < 1 || class > 5 {
		class = 5
	}
	m.requests[class]++
	if !action {
		return
	}
	for i, name := range authorizationOutcomes {
		if name == outcome {
			m.authorizations[i]++
		}
	}
	m.count++
	m.sum += seconds
	for i, bound := range latencyBounds {
		if seconds <= bound {
			m.buckets[i]++
		}
	}
}
func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	h := r.Header.Get("Authorization")
	hash := sha256.Sum256([]byte(strings.TrimPrefix(h, "Bearer ")))
	if !strings.HasPrefix(h, "Bearer ") || subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) != 1 {
		writeJSON(w, 401, map[string]string{"error": "service authentication required"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	s.metrics.mu.Lock()
	snapshot := s.metrics.requests
	outcomes := s.metrics.authorizations
	buckets, sum, count := s.metrics.buckets, s.metrics.sum, s.metrics.count
	s.metrics.mu.Unlock()
	fmt.Fprintln(w, "# HELP counterseal_http_requests_total API endpoint requests by HTTP status class, excluding health and metrics.")
	fmt.Fprintln(w, "# TYPE counterseal_http_requests_total counter")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(w, "counterseal_http_requests_total{status_class=\"%dxx\"} %d\n", i, snapshot[i])
	}
	fmt.Fprintln(w, "# HELP counterseal_authorization_requests_total Action evaluations: committed allow/deny, rejected input/auth, or service error.")
	fmt.Fprintln(w, "# TYPE counterseal_authorization_requests_total counter")
	for i, name := range authorizationOutcomes {
		fmt.Fprintf(w, "counterseal_authorization_requests_total{outcome=%q} %d\n", name, outcomes[i])
	}
	fmt.Fprintln(w, "# HELP counterseal_authorization_duration_seconds Total action endpoint latency including authentication, transaction wait and commit; excludes upstream execution.")
	fmt.Fprintln(w, "# TYPE counterseal_authorization_duration_seconds histogram")
	for i, bound := range latencyBounds {
		fmt.Fprintf(w, "counterseal_authorization_duration_seconds_bucket{le=\"%g\"} %d\n", bound, buckets[i])
	}
	fmt.Fprintf(w, "counterseal_authorization_duration_seconds_bucket{le=\"+Inf\"} %d\ncounterseal_authorization_duration_seconds_sum %g\ncounterseal_authorization_duration_seconds_count %d\n", count, sum, count)
}
