package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"handoffguard/internal/policy"
)

const testToken = "gateway-test-token-at-least-32-bytes"

var inputSchema = json.RawMessage(`{"type":"object","properties":{"order_id":{"type":"string"},"amount":{"type":"integer"}},"required":["order_id","amount"],"additionalProperties":false}`)

func testConfig() Config {
	return Config{Tools: map[string]Mapping{"refund.create": {Action: "refunds.create", Resources: map[string]string{"orders": "/order_id"}, DataClasses: []string{"payment_metadata"}}}}
}
func allow() Authorization {
	return Authorization{ActionID: "action_test", Result: policy.Result{Decision: "ALLOW", Violations: []policy.Violation{}}}
}
func testOptions() Options {
	return Options{AgentID: "billing", EnvelopeID: "env_fixed", Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
}

type fakeUpstream struct {
	calls atomic.Int32
	pages func(*mcp.ListToolsParams) *mcp.ListToolsResult
	call  func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

func (u *fakeUpstream) ListTools(ctx context.Context, p *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	if u.pages != nil {
		return u.pages(p), nil
	}
	return &mcp.ListToolsResult{Tools: []*mcp.Tool{{Name: "refund.create", InputSchema: inputSchema}, {Name: "payment.export", InputSchema: json.RawMessage(`{"type":"object"}`)}}}, nil
}
func (u *fakeUpstream) CallTool(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	u.calls.Add(1)
	if u.call != nil {
		return u.call(ctx, p)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "forwarded"}}, StructuredContent: map[string]any{"ok": true}}, nil
}

type authorizerFunc func(context.Context, Action) (Authorization, error)

func (f authorizerFunc) Authorize(ctx context.Context, a Action) (Authorization, error) {
	return f(ctx, a)
}
func connect(t *testing.T, s *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	left, right := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, left, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := NewUpstreamClient()
	session, err := client.Connect(ctx, right, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func proxy(t *testing.T, u *fakeUpstream, a Authorizer, opts Options) *mcp.ClientSession {
	t.Helper()
	s, err := New(context.Background(), u, a, testConfig(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return connect(t, s)
}
func call(t *testing.T, s *mcp.ClientSession, args any) *mcp.CallToolResult {
	t.Helper()
	result, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "refund.create", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAllowAndExactArguments(t *testing.T) {
	u := &fakeUpstream{}
	var authorized Action
	var forwarded []byte
	u.call = func(ctx context.Context, p *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		forwarded, _ = json.Marshal(p.Arguments)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}, StructuredContent: map[string]any{"receipt": "receipt_1"}}, nil
	}
	a := authorizerFunc(func(ctx context.Context, action Action) (Authorization, error) {
		authorized = action
		return allow(), nil
	})
	s := proxy(t, u, a, testOptions())
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "refund.create" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	args := json.RawMessage(`{"amount":9007199254740993,"order_id":"48319"}`)
	result := call(t, s, args)
	if result.IsError || u.calls.Load() != 1 {
		t.Fatal("allowed call not forwarded")
	}
	expected, _ := json.Marshal(authorized.Arguments)
	if !bytes.Equal(expected, forwarded) || !bytes.Contains(forwarded, []byte("9007199254740993")) {
		t.Fatal("arguments changed")
	}
	if authorized.AgentID != "billing" || authorized.EnvelopeID != "env_fixed" || authorized.Tool != "refunds.create" || authorized.Resources["orders"][0] != "48319" {
		t.Fatalf("incorrect authorization: %+v", authorized)
	}
	if _, err = s.CallTool(context.Background(), &mcp.CallToolParams{Name: "payment.export", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unmapped tool accessible")
	}
	if u.calls.Load() != 1 {
		t.Fatal("unmapped tool forwarded")
	}
}

func TestDenyAndServiceFailuresNeverForward(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision Authorization
		err      error
	}{
		{"deny", Authorization{ActionID: "action_deny", Result: policy.Result{Decision: "DENY", Violations: []policy.Violation{{Code: "APPROVAL_REQUIRED"}}}}, nil},
		{"unavailable", Authorization{}, errors.New("offline")},
		{"missing receipt", Authorization{Result: policy.Result{Decision: "ALLOW"}}, nil},
		{"conflicting violations", Authorization{ActionID: "action_bad", Result: policy.Result{Decision: "ALLOW", Violations: []policy.Violation{{Code: "DENIED"}}}}, nil},
		{"unknown decision", Authorization{ActionID: "action_bad", Result: policy.Result{Decision: "UNKNOWN"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &fakeUpstream{}
			s := proxy(t, u, authorizerFunc(func(context.Context, Action) (Authorization, error) { return tc.decision, tc.err }), testOptions())
			if !call(t, s, map[string]any{"order_id": "48319", "amount": 825}).IsError || u.calls.Load() != 0 {
				t.Fatal("fail-open authorization")
			}
		})
	}
}
func TestInvalidArgumentsNeverAuthorize(t *testing.T) {
	u := &fakeUpstream{}
	var checks atomic.Int32
	s := proxy(t, u, authorizerFunc(func(context.Context, Action) (Authorization, error) { checks.Add(1); return allow(), nil }), testOptions())
	for _, raw := range []string{`{"order_id":"48319","amount":825,"envelope_id":"env_other"}`, `{"order_id":"48319","amount":1,"amount":825}`, `{"order_id":"*","amount":825}`, `{"order_id":48319,"amount":825}`, `{"amount":825}`, `{"order_id":"48319","amount":"825"}`, `null`, `[]`} {
		if !call(t, s, json.RawMessage(raw)).IsError {
			t.Errorf("accepted %s", raw)
		}
	}
	if checks.Load() != 0 || u.calls.Load() != 0 {
		t.Fatal("invalid input caused side effects")
	}
}
func TestUpstreamErrorNoRetryAndSafeLogs(t *testing.T) {
	u := &fakeUpstream{call: func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		return nil, errors.New("secret upstream detail")
	}}
	var logs bytes.Buffer
	options := testOptions()
	options.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	s := proxy(t, u, authorizerFunc(func(context.Context, Action) (Authorization, error) { return allow(), nil }), options)
	result := call(t, s, map[string]any{"order_id": "PRIVATE_ORDER_ID", "amount": 825})
	if !result.IsError || u.calls.Load() != 1 {
		t.Fatal("retry or failure missing")
	}
	if strings.Contains(logs.String(), "PRIVATE_ORDER_ID") || strings.Contains(logs.String(), "825") || strings.Contains(logs.String(), "secret upstream detail") {
		t.Fatal("sensitive data logged")
	}
	if !strings.Contains(logs.String(), "action_test") {
		t.Fatal("missing correlation receipt")
	}
}
func TestCancellationBlocksForwarding(t *testing.T) {
	u := &fakeUpstream{}
	options := testOptions()
	options.Timeout = 10 * time.Millisecond
	s := proxy(t, u, authorizerFunc(func(ctx context.Context, a Action) (Authorization, error) {
		<-ctx.Done()
		return Authorization{}, ctx.Err()
	}), options)
	if !call(t, s, map[string]any{"order_id": "48319", "amount": 825}).IsError || u.calls.Load() != 0 {
		t.Fatal("canceled authorization forwarded")
	}
}
func TestDiscoveryPaginationAndMissingMapping(t *testing.T) {
	u := &fakeUpstream{pages: func(p *mcp.ListToolsParams) *mcp.ListToolsResult {
		if p.Cursor == "" {
			return &mcp.ListToolsResult{Tools: []*mcp.Tool{{Name: "hidden", InputSchema: inputSchema}}, NextCursor: "next"}
		}
		return &mcp.ListToolsResult{Tools: []*mcp.Tool{{Name: "refund.create", InputSchema: inputSchema}}}
	}}
	a := authorizerFunc(func(context.Context, Action) (Authorization, error) { return allow(), nil })
	if _, err := New(context.Background(), u, a, testConfig(), testOptions()); err != nil {
		t.Fatal(err)
	}
	u.pages = func(*mcp.ListToolsParams) *mcp.ListToolsResult { return &mcp.ListToolsResult{NextCursor: "loop"} }
	if _, err := New(context.Background(), u, a, testConfig(), testOptions()); err == nil {
		t.Fatal("pagination cycle accepted")
	}
	u.pages = func(*mcp.ListToolsParams) *mcp.ListToolsResult { return &mcp.ListToolsResult{} }
	if _, err := New(context.Background(), u, a, testConfig(), testOptions()); err == nil {
		t.Fatal("missing configured tool accepted")
	}
	u.pages = func(*mcp.ListToolsParams) *mcp.ListToolsResult {
		return &mcp.ListToolsResult{Tools: []*mcp.Tool{{Name: "refund.create", InputSchema: json.RawMessage(`{"type":"array"}`)}}}
	}
	if _, err := New(context.Background(), u, a, testConfig(), testOptions()); err == nil {
		t.Fatal("invalid schema accepted")
	}
}
func TestResourcePointers(t *testing.T) {
	mapping := Mapping{Resources: map[string]string{"orders": "/a~1b/0/x~0y"}}
	resources, err := extract(map[string]any{"a/b": []any{map[string]any{"x~y": []any{"2", "1", "1"}}}}, mapping)
	if err != nil || fmt.Sprint(resources["orders"]) != "[1 2]" {
		t.Fatalf("%v %v", resources, err)
	}
	for _, pointer := range []string{"order_id", "/~", "/~2"} {
		if _, err := pointerTokens(pointer); err == nil {
			t.Fatal("invalid pointer accepted")
		}
	}
	for _, value := range []any{nil, json.Number("1"), []any{}, []any{1}, "*", " x "} {
		if _, err := extract(map[string]any{"id": value}, Mapping{Resources: map[string]string{"orders": "/id"}}); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
}
func TestConfigAndSecretIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.json")
	raw, _ := json.Marshal(testConfig())
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tools":{},"tools":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("duplicate config accepted")
	}
	t.Setenv("HANDOFFGUARD_API_TOKEN", "secret")
	t.Setenv("HANDOFFGUARD_DATABASE_URL", "private")
	t.Setenv("UPSTREAM_TEST_KEY", "upstream-secret")
	env, err := UpstreamEnvironment([]string{"UPSTREAM_TEST_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(env, "\n"), "HANDOFFGUARD_") {
		t.Fatal("control credentials passed upstream")
	}
	if !strings.Contains(strings.Join(env, "\n"), "UPSTREAM_TEST_KEY=upstream-secret") {
		t.Fatal("explicit upstream credential missing")
	}
	if _, err = UpstreamEnvironment([]string{"HANDOFFGUARD_API_TOKEN"}); err == nil {
		t.Fatal("reserved credential accepted")
	}
}

func TestHTTPAuthorizationContract(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		valid  bool
	}{
		{200, `{"action_id":"action_1","result":{"decision":"ALLOW","violations":[]},"consumed_approval_ids":[]}`, true},
		{403, `{"action_id":"action_1","result":{"decision":"DENY","violations":[]},"consumed_approval_ids":[]}`, true},
		{403, `{"action_id":"action_1","result":{"decision":"ALLOW","violations":[]}}`, false},
		{200, `{"result":{"decision":"ALLOW"}}`, false},
		{200, `{"action_id":"action_1","result":{"decision":"DENY","decision":"ALLOW"}}`, false},
		{500, `{}`, false}, {200, `not json`, false},
	} {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+testToken || r.URL.Path != "/v1/evaluate/action" {
					t.Error("incorrect authorization request")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			a, err := NewHTTPAuthorizer(server.URL, testToken)
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.Authorize(context.Background(), Action{})
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected authorization parsing: %v", err)
			}
		})
	}
}

func TestStreamableHTTPUpstream(t *testing.T) {
	upstream := mcp.NewServer(&mcp.Implementation{Name: "http-test", Version: "1"}, nil)
	var calls atomic.Int32
	upstream.AddTool(&mcp.Tool{Name: "refund.create", InputSchema: inputSchema}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "http result"}}}, nil
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-only-token" {
			t.Error("upstream credentials missing or wrong")
			http.Error(w, "unauthorized", 401)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	transport, err := HTTPTransport(httpServer.URL, "upstream-only-token")
	if err != nil {
		t.Fatal(err)
	}
	startup, cancel := context.WithTimeout(context.Background(), time.Second*5)
	session, err := NewUpstreamClient().Connect(startup, transport, nil)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	proxy, err := New(context.Background(), session, authorizerFunc(func(context.Context, Action) (Authorization, error) { return allow(), nil }), testConfig(), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	downstream := connect(t, proxy)
	result := call(t, downstream, map[string]any{"order_id": "48319", "amount": 100})
	if result.IsError || calls.Load() != 1 {
		t.Fatal("HTTP upstream call not forwarded")
	}
}

func TestPreciseSchemaBoundsAndExternalReferences(t *testing.T) {
	schema, err := compileSchema([]byte(`{"type":"object","properties":{"amount":{"type":"integer","maximum":9007199254740992}},"required":["amount"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(map[string]any{"amount": json.Number("9007199254740993")}); err == nil {
		t.Fatal("rounded large integer bypassed maximum")
	}
	if err = schema.Validate(map[string]any{"amount": json.Number("9007199254740992")}); err != nil {
		t.Fatal(err)
	}
	_, err = compileSchema([]byte(`{"type":"object","properties":{"amount":{"$ref":"file:///etc/passwd"}}}`))
	if err == nil {
		t.Fatal("external schema reference accepted")
	}
}

func TestContinuationIsNotRetried(t *testing.T) {
	u := &fakeUpstream{call: func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{}, RequestState: "retry-state"}, nil
	}}
	s := proxy(t, u, authorizerFunc(func(context.Context, Action) (Authorization, error) { return allow(), nil }), testOptions())
	if !call(t, s, map[string]any{"order_id": "48319", "amount": 100}).IsError || u.calls.Load() != 1 {
		t.Fatal("unsupported continuation was retried")
	}
}

func TestURLAndRedirectRestrictions(t *testing.T) {
	for _, raw := range []string{"http://example.com", "file:///tmp/server", "https://user:secret@example.com", "https://example.com/#fragment"} {
		if ValidateURL(raw) == nil {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
	var called atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	a, err := NewHTTPAuthorizer(redirect.URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Authorize(context.Background(), Action{}); err == nil || called.Load() != 0 {
		t.Fatal("control token followed redirect")
	}
}

func TestNumericWorkBounds(t *testing.T) {
	for _, n := range []string{"1e999999999", "1e-999999999", strings.Repeat("1", 129)} {
		if boundedNumbers(map[string]any{"nested": []any{json.Number(n)}}) {
			t.Fatalf("unbounded number accepted: %s", n)
		}
	}
	for _, n := range []string{"825", "9007199254740993", "0.1", "1e-10"} {
		if !boundedNumbers(json.Number(n)) {
			t.Fatalf("ordinary number rejected: %s", n)
		}
	}
}

func TestSDKClientDoesNotRetryContinuation(t *testing.T) {
	upstream := mcp.NewServer(&mcp.Implementation{Name: "continuation-test", Version: "1"}, nil)
	var calls atomic.Int32
	upstream.AddTool(&mcp.Tool{Name: "refund.create", InputSchema: inputSchema}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{}, RequestState: "do-not-retry"}, nil
	})
	session := connect(t, upstream)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "refund.create", Arguments: map[string]any{"order_id": "48319", "amount": 100}})
	if err != nil {
		t.Fatal(err)
	}
	if result.InputRequests == nil || calls.Load() != 1 {
		t.Fatalf("automatic continuation retried or lost: calls=%d", calls.Load())
	}
}
