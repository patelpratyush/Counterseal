package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"handoffguard/internal/policy"
	"handoffguard/internal/telemetry"
)

type approvalRequest struct {
	EnvelopeID string         `json:"envelope_id"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	Arguments  map[string]any `json:"arguments"`
	ApprovedBy string         `json:"approved_by"`
	Role       string         `json:"role"`
	ExpiresAt  time.Time      `json:"expires_at"`
}

func (s *Server) approve(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	op := currentOperator(ctx)
	if op == nil && !s.AllowDemoApprovals {
		return nil, 0, denied("an authenticated refund manager must approve this request")
	}
	if op != nil && op.Role != "refund_manager" {
		return nil, 0, denied("refund_manager role required")
	}
	var req approvalRequest
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	var operatorID any
	var refundAmount any
	if op != nil {
		if req.ApprovedBy != "" || req.Role != "" {
			return nil, 0, bad("approval identity and role come from the signed-in operator")
		}
		req.ApprovedBy, req.Role, operatorID = op.Username, op.Role, op.ID
		amount, ok := req.Arguments["amount"].(json.Number)
		integer, amountErr := strconv.ParseInt(string(amount), 10, 64)
		order, orderOK := req.Arguments["order_id"].(string)
		if req.Action != "refunds.create" || !ok || amountErr != nil || integer <= 0 || integer > 9007199254740991 || !orderOK || len(req.Arguments) != 2 || req.Resource != "orders:"+order {
			return nil, 0, bad("refund approval requires exact order_id and positive integer amount")
		}
		refundAmount = integer
	}
	v, err := load(ctx, tx, req.EnvelopeID)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	if err = s.active(ctx, tx, v, now); err != nil {
		return nil, 0, err
	}
	if !req.ExpiresAt.After(now) || req.ExpiresAt.After(v.Envelope.ExpiresAt) {
		return nil, 0, bad("approval expiry must be future and within envelope expiry")
	}
	if strings.TrimSpace(req.ApprovedBy) == "" || strings.TrimSpace(req.Role) == "" || req.Arguments == nil {
		return nil, 0, bad("approved_by, role, and exact arguments are required")
	}
	if !has(v.Envelope.AllowedActions, req.Action) || has(v.Envelope.DeniedActions, req.Action) {
		return nil, 0, bad("action not permitted by envelope")
	}
	category, resource, ok := strings.Cut(req.Resource, ":")
	if !ok || !resourceAllowed(v.Envelope.Resources[category], resource) {
		return nil, 0, bad("resource must be an allowed category:id")
	}
	roleKnown := false
	for _, a := range v.Envelope.Approvals {
		if a.RequiredRole == req.Role {
			roleKnown = true
		}
	}
	if !roleKnown {
		return nil, 0, bad("role is not required by this envelope")
	}
	hash, err := digest(req.Arguments)
	if err != nil {
		return nil, 0, err
	}
	if op != nil {
		var duplicate bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM approvals WHERE envelope_id=$1 AND action=$2 AND resource=$3 AND arguments_hash=$4 AND approver_role=$5)", req.EnvelopeID, req.Action, req.Resource, hash, req.Role).Scan(&duplicate); err != nil {
			return nil, 0, err
		}
		if duplicate {
			return nil, 0, &apiError{409, "this exact refund already has an approval; do not approve it twice"}
		}
	}
	id := newID("approval_")
	if _, err = tx.Exec(ctx, "INSERT INTO approvals(id,envelope_id,action,resource,arguments_hash,approver_id,approver_role,expires_at,operator_id,refund_amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", id, req.EnvelopeID, req.Action, req.Resource, hash, req.ApprovedBy, req.Role, req.ExpiresAt, operatorID, refundAmount); err != nil {
		return nil, 0, err
	}
	result := map[string]any{"id": id, "envelope_id": req.EnvelopeID, "action": req.Action, "resource": req.Resource, "arguments_hash": hash, "approved_by": req.ApprovedBy, "role": req.Role, "expires_at": req.ExpiresAt, "status": "APPROVED"}
	if op != nil {
		result["operator_id"] = op.ID
		result["operator_name"] = op.DisplayName
		result["refund_amount"] = refundAmount
	}
	if err = appendAudit(ctx, tx, v.RunID, "approval.created", result); err != nil {
		return nil, 0, err
	}
	return result, 201, nil
}

type actionRequest struct {
	EnvelopeID  string              `json:"envelope_id"`
	AgentID     string              `json:"agent_id"`
	Tool        string              `json:"tool"`
	Resources   map[string][]string `json:"resources"`
	DataClasses []string            `json:"data_classes"`
	Arguments   map[string]any      `json:"arguments"`
}

func (s *Server) action(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var req actionRequest
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	if req.EnvelopeID == "" || req.AgentID == "" || req.Tool == "" || req.Arguments == nil {
		return nil, 0, bad("envelope_id, agent_id, tool, and arguments are required")
	}
	v, err := load(ctx, tx, req.EnvelopeID)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	result := policy.Result{Decision: "ALLOW", Violations: []policy.Violation{}}
	reject := func(code, field, message string) {
		result.Decision = "DENY"
		result.Violations = append(result.Violations, policy.Violation{Code: code, Field: field, Message: message})
	}
	if err = s.active(ctx, tx, v, now); err != nil {
		reject("ENVELOPE_INACTIVE", "envelope_id", err.Error())
	}
	if req.AgentID != v.Envelope.Recipient.Agent {
		reject("AGENT_MISMATCH", "agent_id", "agent is not envelope recipient")
	}
	if has(v.Envelope.DeniedActions, req.Tool) || !has(v.Envelope.AllowedActions, req.Tool) {
		reject("ACTION_DENIED", "tool", "action is not permitted")
	}
	resources := []string{}
	for category, ids := range req.Resources {
		for _, id := range ids {
			if strings.Contains(category, ":") || !resourceAllowed(v.Envelope.Resources[category], id) {
				reject("RESOURCE_DENIED", "resources", "resource is not permitted")
			}
			resources = append(resources, category+":"+id)
		}
	}
	sort.Strings(resources)
	resources = dedupe(resources)
	if len(resources) == 0 {
		reject("RESOURCE_REQUIRED", "resources", "at least one concrete resource is required")
	}
	for _, class := range req.DataClasses {
		if !has(v.Envelope.DataClasses, class) {
			reject("DATA_DENIED", "data_classes", "data class is not permitted")
		}
	}
	argumentsHash, err := digest(req.Arguments)
	if err != nil {
		return nil, 0, err
	}
	// These roots are aliases of the same normalized tool arguments, never
	// separately supplied caller claims that could disagree with the operation.
	args, err := celValues(req.Arguments)
	if err != nil {
		return nil, 0, bad(err.Error())
	}
	roles, err := s.conditions.RequiredRoles(v.Envelope.Approvals, map[string]any{"refund": args, "order": args, "customer": args, "request": args})
	if err != nil {
		reject("CONDITION_ERROR", "approvals", "approval condition could not be evaluated")
	}
	approvalIDs := []string{}
	if result.Decision == "ALLOW" {
		for _, role := range roles {
			for _, resource := range resources {
				var id string
				err := tx.QueryRow(ctx, "SELECT id FROM approvals WHERE envelope_id=$1 AND action=$2 AND resource=$3 AND arguments_hash=$4 AND approver_role=$5 AND expires_at>$6 AND consumed_at IS NULL ORDER BY created_at,id LIMIT 1", req.EnvelopeID, req.Tool, resource, argumentsHash, role, now).Scan(&id)
				if err == pgx.ErrNoRows {
					reject("APPROVAL_REQUIRED", "approvals", fmt.Sprintf("missing valid %s approval for %s", role, resource))
					continue
				}
				if err != nil {
					return nil, 0, err
				}
				approvalIDs = append(approvalIDs, id)
			}
		}
	}
	if result.Decision == "ALLOW" {
		for _, id := range approvalIDs {
			if _, err = tx.Exec(ctx, "UPDATE approvals SET consumed_at=$2 WHERE id=$1", id, now); err != nil {
				return nil, 0, err
			}
		}
	} else {
		approvalIDs = []string{}
	}
	id := newID("action_")
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO tool_actions(id,run_id,envelope_id,agent_id,tool_name,arguments_hash,decision,result) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", id, v.RunID, req.EnvelopeID, req.AgentID, req.Tool, argumentsHash, result.Decision, raw); err != nil {
		return nil, 0, err
	}
	if err = recordViolations(ctx, tx, v.RunID, req.EnvelopeID, result); err != nil {
		return nil, 0, err
	}
	response := map[string]any{"action_id": id, "result": result, "consumed_approval_ids": approvalIDs}
	if err = appendAudit(ctx, tx, v.RunID, "action.evaluated", map[string]any{"trace_id": telemetry.Current(ctx).TraceID, "span_id": telemetry.Current(ctx).SpanID, "action_id": id, "envelope_id": req.EnvelopeID, "agent_id": req.AgentID, "tool": req.Tool, "arguments_hash": argumentsHash, "resources": resources, "data_classes": req.DataClasses, "result": result, "consumed_approval_ids": approvalIDs}); err != nil {
		return nil, 0, err
	}
	if result.Decision == "DENY" {
		return response, 403, nil
	}
	return response, 200, nil
}
func has(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func resourceAllowed(scopes []string, id string) bool {
	if id == "" || strings.ContainsAny(id, "*?[]") || strings.TrimSpace(id) != id {
		return false
	}
	for _, scope := range scopes {
		if scope == "*" || scope == id || (strings.HasSuffix(scope, "/*") && strings.HasPrefix(id, strings.TrimSuffix(scope, "*"))) {
			return true
		}
	}
	return false
}
func dedupe(values []string) []string {
	result := []string{}
	for _, v := range values {
		if len(result) == 0 || result[len(result)-1] != v {
			result = append(result, v)
		}
	}
	return result
}
func celValues(value any) (any, error) {
	switch v := value.(type) {
	case json.Number:
		if n, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			return n, nil
		}
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return n, nil
		}
		// Reject overflowing integral values rather than silently losing precision.
		if !strings.ContainsAny(string(v), ".eE") {
			return nil, fmt.Errorf("integer outside supported range")
		}
		n, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return nil, fmt.Errorf("number outside supported range")
		}
		return n, nil
	case map[string]any:
		out := map[string]any{}
		for key, value := range v {
			converted, err := celValues(value)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, value := range v {
			converted, err := celValues(value)
			if err != nil {
				return nil, err
			}
			out[i] = converted
		}
		return out, nil
	default:
		return value, nil
	}
}
