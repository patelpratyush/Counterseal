package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"handoffguard/internal/telemetry"
)

func TestMCPTracePropagatesToAuthorizationWithoutEnteringArguments(t *testing.T) {
	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	seen := make(chan string, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("traceparent")
		var action Action
		if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
			t.Error(err)
		}
		if len(action.Arguments) != 2 {
			t.Error("trace entered tool arguments")
		}
		json.NewEncoder(w).Encode(allow())
	}))
	defer api.Close()
	auth, err := NewHTTPAuthorizer(api.URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	u := &fakeUpstream{call: func(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		if len(p.GetMeta()) != 0 {
			t.Error("untrusted metadata forwarded upstream")
		}
		return &mcp.CallToolResult{}, nil
	}}
	session := proxy(t, u, auth, testOptions())
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "refund.create", Arguments: map[string]any{"order_id": "48319", "amount": 100}, Meta: mcp.Meta{"traceparent": parent, "secret": "DO_NOT_FORWARD"}})
	if err != nil || result.IsError {
		t.Fatalf("call failed: %v", err)
	}
	child, ok := telemetry.Parse(<-seen)
	if !ok || child.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || child.SpanID == "00f067aa0ba902b7" {
		t.Fatal("missing gateway child span")
	}
}
