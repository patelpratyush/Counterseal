// Package demomcp is a deliberately unguarded, simulated commerce MCP server.
// It never contacts payment systems or stores real customer data.
package demomcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"handoffguard/internal/strictjson"
)

func New() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "acmeshop-unsafe-demo", Version: "0.1.0"}, nil)
	var refunds atomic.Int64
	server.AddTool(&mcp.Tool{Name: "refund.create", Description: "Simulate a refund. This upstream intentionally has no approval checks.", InputSchema: json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string","minLength":1},"amount":{"type":"integer","minimum":1}},"required":["order_id","amount"],"additionalProperties":false}`)}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			OrderID string `json:"order_id"`
			Amount  int64  `json:"amount"`
		}
		if err := strictjson.Decode(req.Params.Arguments, &args); err != nil || args.OrderID == "" || args.Amount <= 0 {
			return errorResult("order_id and a positive integer amount are required"), nil
		}
		result := map[string]any{"refund_id": fmt.Sprintf("simulated_%d", refunds.Add(1)), "order_id": args.OrderID, "amount": args.Amount, "simulated": true}
		raw, _ := json.Marshal(result)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: result}, nil
	})
	server.AddTool(&mcp.Tool{Name: "orders.get", Description: "Read a fixed simulated order.", InputSchema: json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string","minLength":1}},"required":["order_id"],"additionalProperties":false}`), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			OrderID string `json:"order_id"`
		}
		if err := strictjson.Decode(req.Params.Arguments, &args); err != nil || args.OrderID == "" {
			return errorResult("order_id is required"), nil
		}
		result := map[string]any{"order_id": args.OrderID, "total": 1000, "currency": "USD", "simulated": true}
		raw, _ := json.Marshal(result)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}, StructuredContent: result}, nil
	})
	server.AddTool(&mcp.Tool{Name: "payment.export", Description: "Return fake payment data; intentionally omitted from the gateway mapping.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "SIMULATED_PAYMENT_DATA"}}}, nil
	})
	return server
}
func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
