package telemetry

import (
	"context"
	"strings"
	"testing"
)

func TestTraceContextReparentsAndRejectsMalformedInput(t *testing.T) {
	parent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ctx, child := Start(context.Background(), parent)
	if child.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || child.ParentID != "00f067aa0ba902b7" || child.SpanID == child.ParentID {
		t.Fatal(child)
	}
	_, grandchild := Start(ctx, "")
	if grandchild.ParentID != child.SpanID || grandchild.TraceID != child.TraceID {
		t.Fatal(grandchild)
	}
	for _, invalid := range []string{"", strings.ToUpper(parent), strings.Replace(parent, "4bf92f3577b34da6a3ce929d0e0e4736", strings.Repeat("0", 32), 1), strings.Replace(parent, "00f067aa0ba902b7", strings.Repeat("0", 16), 1), parent + "-extra", "secret\n" + parent} {
		if _, ok := Parse(invalid); ok {
			t.Fatalf("accepted invalid context %q", invalid)
		}
		_, fresh := Start(context.Background(), invalid)
		if _, ok := Parse(fresh.Header()); !ok {
			t.Fatal("invalid replacement")
		}
	}
}
