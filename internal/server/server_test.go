package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"handoffguard/internal/envelope"
	"handoffguard/internal/store"
)

const testToken = "integration-test-token-at-least-32-bytes"

type fixture struct {
	db   *store.Store
	http *httptest.Server
	key  ed25519.PrivateKey
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("HANDOFFGUARD_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set HANDOFFGUARD_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := newID("test_")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	db := &store.Store{Pool: pool}
	t.Cleanup(db.Close)
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, db, key, testToken)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(api.Handler())
	t.Cleanup(httpServer.Close)
	return &fixture{db, httpServer, key}
}
func (f *fixture) request(method, path string, body any) (int, map[string]any, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
	}
	req, err := http.NewRequest(method, f.http.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	var value map[string]any
	if err = json.NewDecoder(response.Body).Decode(&value); err != nil {
		return response.StatusCode, nil, err
	}
	return response.StatusCode, value, nil
}
func (f *fixture) must(t *testing.T, method, path string, body any, status int) map[string]any {
	t.Helper()
	code, value, err := f.request(method, path, body)
	if err != nil || code != status {
		t.Fatalf("%s %s: status=%d body=%v err=%v", method, path, code, value, err)
	}
	return value
}
func draft() envelope.Envelope {
	return envelope.Envelope{Version: "1", Issuer: envelope.Party{Agent: "support"}, Recipient: envelope.Party{Agent: "billing"}, Purpose: "refund", PolicyVersion: "refund-v1", ExpiresAt: time.Now().UTC().Add(time.Hour), AllowedActions: []string{"refunds.create", "orders.read"}, DeniedActions: []string{"customers.delete"}, Resources: map[string][]string{"orders": {"48319"}}, DataClasses: []string{"pii"}, Approvals: []envelope.Approval{{Condition: "refund.amount > 500", RequiredRole: "manager"}}, Delegation: envelope.Delegation{MaxDepth: 3}}
}
func (f *fixture) root(t *testing.T) (string, string) {
	t.Helper()
	result := f.must(t, "POST", "/v1/envelopes", map[string]any{"envelope": draft()}, 201)
	return result["envelope"].(map[string]any)["id"].(string), result["run_id"].(string)
}
func actionBody(id string) map[string]any {
	return map[string]any{"envelope_id": id, "agent_id": "billing", "tool": "refunds.create", "resources": map[string][]string{"orders": {"48319"}}, "data_classes": []string{"pii"}, "arguments": map[string]any{"order_id": "48319", "amount": 825, "private_note": "DO_NOT_STORE_RAW_ARGUMENTS"}}
}
func approvalBody(id string) map[string]any {
	return map[string]any{"envelope_id": id, "action": "refunds.create", "resource": "orders:48319", "arguments": actionBody(id)["arguments"], "approved_by": "manager_1", "role": "manager", "expires_at": time.Now().Add(20 * time.Minute)}
}

func TestPostgresLifecycle(t *testing.T) {
	f := setup(t)
	id, run := f.root(t)
	f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 403)
	approval := f.must(t, "POST", "/v1/approvals", approvalBody(id), 201)
	// Two requests compete for a single approval. Exactly one may consume it.
	codes := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _, err := f.request("POST", "/v1/evaluate/action", actionBody(id))
			codes <- code
			errs <- err
		}()
	}
	wg.Wait()
	close(codes)
	close(errs)
	allowed, deniedCount := 0, 0
	for code := range codes {
		if code == 200 {
			allowed++
		} else if code == 403 {
			deniedCount++
		} else {
			t.Fatalf("unexpected status %d", code)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if allowed != 1 || deniedCount != 1 {
		t.Fatalf("approval replay allowed: %d/%d", allowed, deniedCount)
	}
	var consumed *time.Time
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT consumed_at FROM approvals WHERE id=$1", approval["id"]).Scan(&consumed); err != nil || consumed == nil {
		t.Fatalf("approval not consumed: %v", err)
	}
	audit := f.must(t, "POST", "/v1/audit/"+run+"/verify", nil, 200)
	if audit["status"] != "VALID" {
		t.Fatalf("audit invalid: %v", audit)
	}
	// Raw arguments are never retained in action rows or audit payloads.
	var leaked bool
	err := f.db.Pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM audit_events WHERE payload::text LIKE '%DO_NOT_STORE_RAW_ARGUMENTS%') OR EXISTS(SELECT 1 FROM tool_actions WHERE result::text LIKE '%DO_NOT_STORE_RAW_ARGUMENTS%')").Scan(&leaked)
	if err != nil || leaked {
		t.Fatalf("raw arguments leaked: %v", err)
	}
	// Stored signatures survive a fresh service instance with the same key.
	api, err := New(context.Background(), f.db, f.key, testToken)
	if err != nil {
		t.Fatal(err)
	}
	restarted := httptest.NewServer(api.Handler())
	defer restarted.Close()
	original := f.http
	f.http = restarted
	f.must(t, "GET", "/v1/envelopes/"+id, nil, 200)
	f.http = original
	_, wrong, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = New(context.Background(), f.db, wrong, testToken); err == nil {
		t.Fatal("key rotation silently accepted")
	}
}

func TestDelegationRevocationAndAuditTampering(t *testing.T) {
	f := setup(t)
	id, run := f.root(t)
	child := draft()
	child.Issuer = child.Recipient
	child.Recipient = envelope.Party{Agent: "notifications"}
	child.ParentEnvelope = id
	child.Delegation.CurrentDepth = 1
	child.ExpiresAt = child.ExpiresAt.Add(-time.Minute)
	response := f.must(t, "POST", "/v1/envelopes/"+id+"/delegate", map[string]any{"child": child}, 201)
	childID := response["envelope"].(map[string]any)["id"].(string)
	sibling := f.must(t, "POST", "/v1/envelopes/"+id+"/delegate", map[string]any{"child": child}, 201)
	siblingID := sibling["envelope"].(map[string]any)["id"].(string)
	expanded := child
	expanded.AllowedActions = append(append([]string{}, child.AllowedActions...), "orders.delete")
	f.must(t, "POST", "/v1/envelopes/"+id+"/delegate", map[string]any{"child": expanded}, 403)
	f.must(t, "GET", "/v1/runs/"+run+"/chain", nil, 200)
	grandchild := child
	grandchild.Issuer = child.Recipient
	grandchild.Recipient = envelope.Party{Agent: "worker"}
	grandchild.ParentEnvelope = childID
	grandchild.Delegation.CurrentDepth = 2
	grand := f.must(t, "POST", "/v1/envelopes/"+childID+"/delegate", map[string]any{"child": grandchild}, 201)
	grandID := grand["envelope"].(map[string]any)["id"].(string)
	f.must(t, "POST", "/v1/envelopes/"+childID+"/revoke", nil, 200)
	for _, item := range []struct{ id, agent string }{{childID, "notifications"}, {grandID, "worker"}} {
		request := actionBody(item.id)
		request["agent_id"] = item.agent
		request["arguments"] = map[string]any{"amount": 100}
		f.must(t, "POST", "/v1/evaluate/action", request, 403)
	}
	request := actionBody(siblingID)
	request["agent_id"] = "notifications"
	request["arguments"] = map[string]any{"amount": 100}
	f.must(t, "POST", "/v1/evaluate/action", request, 200)
	audit := f.must(t, "POST", "/v1/audit/"+run+"/verify", nil, 200)
	if audit["status"] != "VALID" {
		t.Fatalf("historical audit invalid: %v", audit)
	}
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE audit_events SET payload='{}' WHERE seq=(SELECT min(seq) FROM audit_events WHERE run_id=$1)", run); err != nil {
		t.Fatal(err)
	}
	audit = f.must(t, "POST", "/v1/audit/"+run+"/verify", nil, 200)
	if audit["status"] != "INVALID" {
		t.Fatal("audit tampering not detected")
	}
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE envelopes SET document=jsonb_set(document,'{purpose}','\"tampered\"') WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	request = actionBody(id)
	request["arguments"] = map[string]any{"amount": 100}
	f.must(t, "POST", "/v1/evaluate/action", request, 403)
}

func TestAuthorizationFailuresAndRollback(t *testing.T) {
	f := setup(t)
	id, _ := f.root(t)
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["agent_id"] = "imposter" },
		func(v map[string]any) { v["tool"] = "customers.delete" },
		func(v map[string]any) { v["resources"] = map[string][]string{"orders": {"other"}} },
		func(v map[string]any) { v["resources"] = map[string][]string{"orders": {"*"}} },
		func(v map[string]any) { v["resources"] = nil },
		func(v map[string]any) { v["data_classes"] = []string{"secret"} },
		func(v map[string]any) { v["arguments"] = map[string]any{} },
	} {
		req := actionBody(id)
		mutate(req)
		f.must(t, "POST", "/v1/evaluate/action", req, 403)
	}
	approval := f.must(t, "POST", "/v1/approvals", approvalBody(id), 201)
	changed := actionBody(id)
	changed["arguments"].(map[string]any)["amount"] = 826
	f.must(t, "POST", "/v1/evaluate/action", changed, 403)
	// Force a failure after approval consumption and action insertion. Every
	// write, including the consumption, must roll back with the audit failure.
	if _, err := f.db.Pool.Exec(context.Background(), "ALTER TABLE audit_events ADD CONSTRAINT fail_action CHECK (event_type <> 'action.evaluated') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 500)
	var consumed *time.Time
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT consumed_at FROM approvals WHERE id=$1", approval["id"]).Scan(&consumed); err != nil || consumed != nil {
		t.Fatalf("approval consumed despite rollback: %v", err)
	}
	if _, err := f.db.Pool.Exec(context.Background(), "ALTER TABLE audit_events DROP CONSTRAINT fail_action"); err != nil {
		t.Fatal(err)
	}
	f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 200)
}

func TestHTTPValidation(t *testing.T) {
	f := setup(t)
	response, err := http.Get(f.http.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("health check failed")
	}
	response, err = http.Post(f.http.URL+"/v1/envelopes", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("missing token accepted")
	}
	for _, body := range []string{`{"envelope":{},"envelope":{}}`, `{"unknown":1}`, `{} {}`, strings.Repeat(" ", 1<<20) + `{}`} {
		req, _ := http.NewRequest("POST", f.http.URL+"/v1/envelopes", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("invalid input returned %d", res.StatusCode)
		}
	}
	f.must(t, "GET", "/v1/envelopes/missing", nil, 404)
	f.must(t, "POST", "/v1/audit/missing/verify", nil, 404)
	if _, err := New(context.Background(), f.db, f.key, "short"); err == nil {
		t.Fatal("weak token accepted")
	}
}

func TestUniqueJSON(t *testing.T) {
	for _, raw := range []string{`{"id":1,"ID":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `[1,`} {
		if uniqueJSON([]byte(raw)) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := uniqueJSON([]byte(`{"x":[1,2,{"ok":true}]}`)); err != nil {
		t.Fatal(err)
	}
}
func TestCELNumbers(t *testing.T) {
	for _, number := range []string{"825", "825.5", "18446744073709551615"} {
		if _, err := celValues(json.Number(number)); err != nil {
			t.Fatal(err)
		}
	}
	for _, number := range []string{"18446744073709551616", "1e999"} {
		if _, err := celValues(json.Number(number)); err == nil {
			t.Fatal(fmt.Sprintf("accepted %s", number))
		}
	}
}

func TestApprovalScopeExpiryAndReadAPIs(t *testing.T) {
	f := setup(t)
	id, run := f.root(t)
	other, _ := f.root(t)
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["expires_at"] = time.Now().Add(-time.Minute) },
		func(v map[string]any) { v["expires_at"] = time.Now().Add(2 * time.Hour) },
		func(v map[string]any) { v["resource"] = "orders:other" },
		func(v map[string]any) { v["action"] = "customers.delete" },
		func(v map[string]any) { v["role"] = "intern" },
		func(v map[string]any) { v["approved_by"] = "" },
		func(v map[string]any) { v["arguments"] = nil },
	} {
		req := approvalBody(id)
		mutate(req)
		f.must(t, "POST", "/v1/approvals", req, 400)
	}
	approval := f.must(t, "POST", "/v1/approvals", approvalBody(id), 201)
	f.must(t, "POST", "/v1/evaluate/action", actionBody(other), 403)
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE approvals SET expires_at=now()-interval '1 second' WHERE id=$1", approval["id"]); err != nil {
		t.Fatal(err)
	}
	f.must(t, "POST", "/v1/evaluate/action", actionBody(id), 403)
	// Validation failures and duplicate roots cannot leave a partial run.
	invalid := draft()
	invalid.ExpiresAt = time.Now().Add(-time.Hour)
	f.must(t, "POST", "/v1/envelopes", map[string]any{"envelope": invalid}, 422)
	invalid = draft()
	invalid.Resources = map[string][]string{"orders:special": {"48319"}}
	f.must(t, "POST", "/v1/envelopes", map[string]any{"envelope": invalid}, 400)
	invalid = draft()
	invalid.ParentEnvelope = id
	f.must(t, "POST", "/v1/envelopes", map[string]any{"envelope": invalid}, 400)
	f.must(t, "POST", "/v1/envelopes", map[string]any{"envelope": draft(), "run_id": run}, 409)
	root := f.must(t, "GET", "/v1/envelopes/"+id, nil, 200)
	raw, _ := json.Marshal(root["envelope"])
	var parent envelope.Envelope
	if err := json.Unmarshal(raw, &parent); err != nil {
		t.Fatal(err)
	}
	child := parent
	child.ID = "env_dryrun"
	child.ParentEnvelope = id
	child.Issuer = parent.Recipient
	child.Recipient = envelope.Party{Agent: "worker"}
	child.Delegation.CurrentDepth = 1
	f.must(t, "POST", "/v1/evaluate/handoff", map[string]any{"parent_envelope_id": id, "child": child}, 200)
	f.must(t, "GET", "/v1/envelopes/"+child.ID, nil, 404)
	f.must(t, "POST", "/v1/policies/diff", map[string]any{"parent": parent, "child": child}, 200)
	graph := f.must(t, "GET", "/v1/runs/"+run+"/chain", nil, 200)
	if len(graph["handoffs"].([]any)) != 1 || len(graph["actions"].([]any)) != 1 {
		t.Fatalf("graph missing decisions: %v", graph)
	}
	req, _ := http.NewRequest("GET", f.http.URL+"/v1/runs/"+run+"/violations", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var violations []any
	if err = json.NewDecoder(response.Body).Decode(&violations); err != nil || len(violations) == 0 || response.StatusCode != 200 {
		t.Fatalf("violations missing: %v", err)
	}
	f.must(t, "POST", "/v1/envelopes/"+id+"/revoke", nil, 200)
	f.must(t, "POST", "/v1/envelopes/"+id+"/revoke", nil, 200)
	f.must(t, "POST", "/v1/envelopes/"+id+"/delegate", map[string]any{"child": child}, 403)
	f.must(t, "POST", "/v1/approvals", approvalBody(id), 403)
}

func TestAuditDetectsDeletedEnvelope(t *testing.T) {
	f := setup(t)
	id, run := f.root(t)
	if _, err := f.db.Pool.Exec(context.Background(), "DELETE FROM envelopes WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	result := f.must(t, "POST", "/v1/audit/"+run+"/verify", nil, 200)
	if result["status"] != "INVALID" {
		t.Fatal("deleted envelope not detected")
	}
}

func TestServerRejectsMalformedKeysBeforeDatabaseAccess(t *testing.T) {
	if _, err := New(context.Background(), nil, nil, testToken); err == nil {
		t.Fatal("accepted missing key")
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key[len(key)-1] ^= 1
	if _, err = New(context.Background(), nil, key, testToken); err == nil {
		t.Fatal("accepted corrupted private key")
	}
}
