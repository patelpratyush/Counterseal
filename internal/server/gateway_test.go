package server

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"handoffguard/internal/demomcp"
	"handoffguard/internal/gateway"
)

func mcpSession(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	a, b := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	session, err := gateway.NewUpstreamClient().Connect(context.Background(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func TestGatewayPostgresEndToEnd(t *testing.T) {
	f := setup(t)
	id, run := f.root(t)
	upstream := mcpSession(t, demomcp.New())
	args := map[string]any{"order_id": "48319", "amount": 825}
	invoke := func(session *mcp.ClientSession, name string, arguments any) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	direct := invoke(upstream, "refund.create", args)
	if direct.IsError {
		t.Fatal("unsafe upstream unexpectedly enforced approvals")
	}
	if direct.StructuredContent.(map[string]any)["refund_id"] != "simulated_1" {
		t.Fatal("unexpected direct refund")
	}
	auth, err := gateway.NewHTTPAuthorizer(f.http.URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	config := gateway.Config{Tools: map[string]gateway.Mapping{"refund.create": {Action: "refunds.create", Resources: map[string]string{"orders": "/order_id"}, DataClasses: []string{"pii"}}}}
	proxy, err := gateway.New(context.Background(), upstream, auth, config, gateway.Options{AgentID: "billing", EnvelopeID: id, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	client := mcpSession(t, proxy)
	tools, err := client.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 1 {
		t.Fatalf("discovery: %v %v", tools, err)
	}
	if !invoke(client, "refund.create", args).IsError {
		t.Fatal("unapproved refund forwarded")
	}
	approved := approvalBody(id)
	approved["arguments"] = args
	f.must(t, "POST", "/v1/approvals", approved, 201)
	allowed := invoke(client, "refund.create", args)
	if allowed.IsError {
		t.Fatalf("approved refund denied: %+v", allowed.Content)
	}
	if allowed.StructuredContent.(map[string]any)["refund_id"] != "simulated_2" {
		t.Fatal("denied call reached upstream")
	}
	if !invoke(client, "refund.create", args).IsError {
		t.Fatal("approval replay forwarded")
	}
	// Caller-controlled identity fields cannot replace the fixed session identity.
	if !invoke(client, "refund.create", map[string]any{"order_id": "48319", "amount": 100, "envelope_id": "another"}).IsError {
		t.Fatal("spoofed argument accepted")
	}
	if _, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "payment.export", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unmapped export exposed")
	}
	f.must(t, "POST", "/v1/envelopes/"+id+"/revoke", nil, 200)
	if !invoke(client, "refund.create", map[string]any{"order_id": "48319", "amount": 100}).IsError {
		t.Fatal("revoked envelope forwarded")
	}
	audit := f.must(t, "POST", "/v1/audit/"+run+"/verify", nil, 200)
	if audit["status"] != "VALID" {
		t.Fatalf("audit invalid: %v", audit)
	}
	var allowedCount, deniedCount int
	err = f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FILTER(WHERE decision='ALLOW'),count(*) FILTER(WHERE decision='DENY') FROM tool_actions WHERE run_id=$1", run).Scan(&allowedCount, &deniedCount)
	if err != nil || allowedCount != 1 || deniedCount != 3 {
		t.Fatalf("unexpected persisted decisions: %d/%d %v", allowedCount, deniedCount, err)
	}
}
