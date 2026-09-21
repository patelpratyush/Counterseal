package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const operatorPassword = "operator-test-password-32-bytes!"

func strictOperators(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	api, err := New(context.Background(), f.db, f.key, testToken)
	if err != nil {
		t.Fatal(err)
	}
	f.http = httptest.NewServer(api.Handler())
	t.Cleanup(f.http.Close)
	for _, user := range []struct{ username, role string }{{"morgan", "refund_manager"}, {"reader", "viewer"}} {
		if err := ProvisionOperator(context.Background(), f.db, user.username, user.username, user.role, operatorPassword, false); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func opRequest(t *testing.T, f *fixture, token, method, path string, body any, want int) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, f.http.URL+path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result json.RawMessage
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, result)
	}
	return result
}
func opLogin(t *testing.T, f *fixture, user string) string {
	raw := opRequest(t, f, "", "POST", "/v1/operators/login", map[string]string{"username": user, "password": operatorPassword}, 200)
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v.Token
}
func TestOperatorApprovalIdentityAndScope(t *testing.T) {
	f := strictOperators(t)
	manager := opLogin(t, f, "morgan")
	viewer := opLogin(t, f, "reader")
	envelope := draft()
	envelope.Approvals[0].RequiredRole = "refund_manager"
	root := f.must(t, "POST", "/v1/envelopes", map[string]any{"envelope": envelope}, 201)
	id := root["envelope"].(map[string]any)["id"].(string)
	run := root["run_id"].(string)
	body := map[string]any{"envelope_id": id, "action": "refunds.create", "resource": "orders:48319", "arguments": map[string]any{"order_id": "48319", "amount": 825}, "expires_at": time.Now().Add(10 * time.Minute)}
	opRequest(t, f, testToken, "POST", "/v1/approvals", body, 403)
	opRequest(t, f, viewer, "POST", "/v1/approvals", body, 403)
	body["approved_by"] = "someone-else"
	opRequest(t, f, manager, "POST", "/v1/approvals", body, 400)
	delete(body, "approved_by")
	body["role"] = "refund_manager"
	opRequest(t, f, manager, "POST", "/v1/approvals", body, 400)
	delete(body, "role")
	raw := opRequest(t, f, manager, "POST", "/v1/approvals", body, 201)
	if !bytes.Contains(raw, []byte(`"approved_by":"morgan"`)) || !bytes.Contains(raw, []byte(`"operator_id":"operator_`)) {
		t.Fatalf("missing trusted identity: %s", raw)
	}
	opRequest(t, f, manager, "POST", "/v1/approvals", body, 409)
	action := actionBody(id)
	action["arguments"] = map[string]any{"order_id": "48319", "amount": 826}
	f.must(t, "POST", "/v1/evaluate/action", action, 403)
	action["arguments"] = body["arguments"]
	f.must(t, "POST", "/v1/evaluate/action", action, 200)
	f.must(t, "POST", "/v1/evaluate/action", action, 403)
	opRequest(t, f, manager, "POST", "/v1/approvals", body, 409)
	history := opRequest(t, f, viewer, "GET", "/v1/runs/"+run+"/approvals", nil, 200)
	if !bytes.Contains(history, []byte(`"username":"morgan"`)) || bytes.Contains(history, []byte(`"consumed_at":null`)) {
		t.Fatalf("missing consumed attributed history: %s", history)
	}
	audit := f.must(t, "POST", "/v1/audit/"+run+"/verify", nil, 200)
	if audit["status"] != "VALID" {
		t.Fatal(audit)
	}
	var stored string
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT payload::text FROM audit_events WHERE run_id=$1 AND event_type='approval.created'", run).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "operator_id") || !strings.Contains(stored, "morgan") {
		t.Fatal("audit omitted authenticated actor")
	}
	opRequest(t, f, manager, "POST", "/v1/envelopes", map[string]any{"envelope": draft()}, 403)
	opRequest(t, f, viewer, "GET", "/v1/dashboard/overview", nil, 200)
}
func TestOperatorSessionsAreRevocableAndStoredAsHashes(t *testing.T) {
	f := strictOperators(t)
	opRequest(t, f, "", "POST", "/v1/operators/login", map[string]string{"username": "morgan", "password": "wrong"}, 401)
	token := opLogin(t, f, "morgan")
	opRequest(t, f, token, "GET", "/v1/operators/me", nil, 200)
	var hash string
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT token_hash FROM operator_sessions LIMIT 1").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == token || hash != tokenDigest(token) {
		t.Fatal("session was not hashed")
	}
	opRequest(t, f, token, "POST", "/v1/operators/logout", nil, 200)
	opRequest(t, f, token, "GET", "/v1/operators/me", nil, 401)
	token = opLogin(t, f, "morgan")
	if err := ChangeOperator(context.Background(), f.db, "morgan", operatorPassword, false); err != nil {
		t.Fatal(err)
	}
	opRequest(t, f, token, "GET", "/v1/operators/me", nil, 401)
	token = opLogin(t, f, "morgan")
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE operator_sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	opRequest(t, f, token, "GET", "/v1/operators/me", nil, 401)
	token = opLogin(t, f, "morgan")
	if err := ChangeOperator(context.Background(), f.db, "morgan", "", true); err != nil {
		t.Fatal(err)
	}
	opRequest(t, f, token, "GET", "/v1/operators/me", nil, 401)
	opRequest(t, f, "", "POST", "/v1/operators/login", map[string]string{"username": "morgan", "password": operatorPassword}, 401)
}
func TestLoginLimitPersistsAcrossServerInstances(t *testing.T) {
	f := strictOperators(t)
	for i := 0; i < 10; i++ {
		opRequest(t, f, "", "POST", "/v1/operators/login", map[string]string{"username": "missing", "password": "incorrect-password"}, 401)
	}
	api, err := New(context.Background(), f.db, f.key, testToken)
	if err != nil {
		t.Fatal(err)
	}
	f.http = httptest.NewServer(api.Handler())
	t.Cleanup(f.http.Close)
	opRequest(t, f, "", "POST", "/v1/operators/login", map[string]string{"username": "missing", "password": "incorrect-password"}, 429)
}
func TestPasswordHashUsesSaltAndRejectsMalformedEncoding(t *testing.T) {
	one, err := passwordHash(operatorPassword)
	if err != nil {
		t.Fatal(err)
	}
	two, err := passwordHash(operatorPassword)
	if err != nil {
		t.Fatal(err)
	}
	if one == two || !passwordValid(operatorPassword, one) || passwordValid("wrong", one) || passwordValid(operatorPassword, "malformed") {
		t.Fatal("password verification failed")
	}
}
