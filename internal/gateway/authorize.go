package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"handoffguard/internal/policy"
	"handoffguard/internal/strictjson"
	"handoffguard/internal/telemetry"
)

type Action struct {
	EnvelopeID  string              `json:"envelope_id"`
	AgentID     string              `json:"agent_id"`
	Tool        string              `json:"tool"`
	Resources   map[string][]string `json:"resources"`
	DataClasses []string            `json:"data_classes"`
	Arguments   map[string]any      `json:"arguments"`
}
type Authorization struct {
	ActionID            string        `json:"action_id"`
	Result              policy.Result `json:"result"`
	ConsumedApprovalIDs []string      `json:"consumed_approval_ids"`
}
type Authorizer interface {
	Authorize(context.Context, Action) (Authorization, error)
}
type HTTPAuthorizer struct {
	endpoint, token string
	client          *http.Client
}

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("invalid server URL")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return fmt.Errorf("use HTTPS, or HTTP on loopback for local development")
}
func NewHTTPAuthorizer(base, token string) (*HTTPAuthorizer, error) {
	if err := ValidateURL(base); err != nil {
		return nil, err
	}
	u, _ := url.Parse(base)
	if u.RawQuery != "" {
		return nil, fmt.Errorf("control API base URL must not contain a query")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("HANDOFFGUARD_API_TOKEN must be at least 32 bytes")
	}
	return &HTTPAuthorizer{endpoint: strings.TrimRight(base, "/") + "/v1/evaluate/action", token: token, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (a *HTTPAuthorizer) Authorize(ctx context.Context, action Action) (Authorization, error) {
	var decision Authorization
	raw, err := json.Marshal(action)
	if err != nil {
		return decision, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.endpoint, bytes.NewReader(raw))
	if err != nil {
		return decision, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	if parent := telemetry.Current(ctx).Header(); parent != "" {
		req.Header.Set("traceparent", parent)
	}
	response, err := a.client.Do(req)
	if err != nil {
		return decision, fmt.Errorf("authorization service unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 403 {
		return decision, fmt.Errorf("authorization service returned HTTP %d", response.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return decision, fmt.Errorf("invalid authorization response")
	}
	if err = strictjson.Decode(raw, &decision); err != nil {
		return decision, fmt.Errorf("invalid authorization response")
	}
	if decision.ActionID == "" {
		return decision, fmt.Errorf("authorization response missing action receipt")
	}
	if response.StatusCode == 200 && decision.Result.Decision == "ALLOW" && len(decision.Result.Violations) == 0 {
		return decision, nil
	}
	if response.StatusCode == 403 && decision.Result.Decision == "DENY" {
		return decision, nil
	}
	return decision, fmt.Errorf("inconsistent authorization response")
}
