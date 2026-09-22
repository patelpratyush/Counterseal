// Package telemetry provides version-00 trace context and structured local spans.
// Trace context is correlation data, never an authorization input.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

type Span struct {
	TraceID, SpanID, ParentID, Flags string
	started                          time.Time
}
type contextKey struct{}

var parentFormat = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$`)

func Parse(parent string) (Span, bool) {
	parts := parentFormat.FindStringSubmatch(parent)
	if parts == nil || parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) {
		return Span{}, false
	}
	return Span{TraceID: parts[1], SpanID: parts[2], Flags: parts[3]}, true
}
func randomID(n int) string            { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func Current(ctx context.Context) Span { s, _ := ctx.Value(contextKey{}).(Span); return s }
func Start(ctx context.Context, parent string) (context.Context, Span) {
	p, ok := Parse(parent)
	if !ok {
		p = Current(ctx)
	}
	s := Span{TraceID: p.TraceID, ParentID: p.SpanID, SpanID: randomID(8), Flags: p.Flags, started: time.Now()}
	if s.TraceID == "" {
		s.TraceID, s.Flags = randomID(16), "01"
	}
	return context.WithValue(ctx, contextKey{}, s), s
}
func (s Span) Header() string {
	if s.TraceID == "" {
		return ""
	}
	return "00-" + s.TraceID + "-" + s.SpanID + "-" + s.Flags
}
func (s Span) End(logger *slog.Logger, service, name, outcome string) {
	if logger == nil {
		return
	}
	logger.Info("trace.span", "service", service, "name", name, "trace_id", s.TraceID,
		"span_id", s.SpanID, "parent_span_id", s.ParentID, "duration_ms", float64(time.Since(s.started).Microseconds())/1000, "outcome", outcome)
}
