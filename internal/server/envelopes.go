package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"handoffguard/internal/envelope"
	"handoffguard/internal/policy"
)

type storedEnvelope struct {
	Envelope  envelope.Envelope `json:"envelope"`
	RunID     string            `json:"run_id"`
	RevokedAt *time.Time        `json:"revoked_at,omitempty"`
	parent    *string
}

func load(ctx context.Context, tx pgx.Tx, id string) (storedEnvelope, error) {
	var v storedEnvelope
	var raw []byte
	err := tx.QueryRow(ctx, "SELECT document,run_id,revoked_at,parent_id FROM envelopes WHERE id=$1", id).Scan(&raw, &v.RunID, &v.RevokedAt, &v.parent)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v.Envelope)
	if err == nil && v.Envelope.ID != id {
		err = fmt.Errorf("stored envelope id mismatch")
	}
	return v, err
}
func (s *Server) active(ctx context.Context, tx pgx.Tx, v storedEnvelope, now time.Time) error {
	run := v.RunID
	seen := map[string]bool{}
	for depth := 0; depth <= 128; depth++ {
		e := v.Envelope
		if seen[e.ID] {
			return denied("cyclic parent chain")
		}
		seen[e.ID] = true
		if v.RunID != run {
			return denied("parent run mismatch")
		}
		if v.RevokedAt != nil {
			return denied("envelope or ancestor revoked")
		}
		if err := envelope.Verify(e, s.pub); err != nil {
			return denied("invalid stored envelope signature")
		}
		if result := s.engine.Validate(e, now); result.Decision != "ALLOW" {
			return denied("invalid or expired envelope")
		}
		if v.parent == nil {
			if e.ParentEnvelope != "" || e.Delegation.CurrentDepth != 0 {
				return denied("invalid root")
			}
			return nil
		}
		if *v.parent != e.ParentEnvelope {
			return denied("parent reference mismatch")
		}
		parent, err := load(ctx, tx, e.ParentEnvelope)
		if err != nil {
			return err
		}
		if result := s.engine.Diff(parent.Envelope, e, now); result.Decision != "ALLOW" {
			return denied("invalid stored delegation")
		}
		v = parent
	}
	return denied("delegation chain too deep")
}
func (s *Server) save(ctx context.Context, tx pgx.Tx, run string, v envelope.Envelope) error {
	for _, party := range []envelope.Party{v.Issuer, v.Recipient} {
		if _, err := tx.Exec(ctx, "INSERT INTO agents(name,version) VALUES($1,$2) ON CONFLICT DO NOTHING", party.Agent, party.Version); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var parent any
	if v.ParentEnvelope != "" {
		parent = v.ParentEnvelope
	}
	_, err = tx.Exec(ctx, "INSERT INTO envelopes(id,run_id,parent_id,document) VALUES($1,$2,$3,$4)", v.ID, run, parent, raw)
	return err
}
func (s *Server) create(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var req struct {
		RunID    string            `json:"run_id"`
		Envelope envelope.Envelope `json:"envelope"`
	}
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	v := req.Envelope
	if req.RunID == "" {
		req.RunID = newID("run_")
	}
	if v.ID == "" {
		v.ID = envelope.NewID()
	}
	if v.Version == "" {
		v.Version = "1"
	}
	if v.ParentEnvelope != "" || v.Delegation.CurrentDepth != 0 || v.Delegation.MaxDepth > 128 {
		return nil, 0, bad("root requires no parent, current_depth 0, and max_depth <= 128")
	}
	for category := range v.Resources {
		if strings.Contains(category, ":") {
			return nil, 0, bad("resource categories must not contain ':'")
		}
	}
	if result := s.engine.Validate(v, time.Now()); result.Decision != "ALLOW" {
		return result, 422, nil
	}
	var err error
	v.Signature, err = envelope.Sign(v, s.key)
	if err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO runs(id) VALUES($1)", req.RunID); err != nil {
		return nil, 0, err
	}
	if err = s.save(ctx, tx, req.RunID, v); err != nil {
		return nil, 0, err
	}
	if err = appendAudit(ctx, tx, req.RunID, "envelope.created", v); err != nil {
		return nil, 0, err
	}
	return storedEnvelope{Envelope: v, RunID: req.RunID}, 201, nil
}
func (s *Server) get(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	v, err := load(ctx, tx, r.PathValue("id"))
	return v, 200, err
}

// Delegation accepts the complete proposed child constraints; missing fields
// never implicitly widen or silently copy authority.
type handoffRequest struct {
	ParentID string            `json:"parent_envelope_id"`
	Child    envelope.Envelope `json:"child"`
}

func (s *Server) delegate(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var req struct {
		Child envelope.Envelope `json:"child"`
	}
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	return s.evaluateHandoff(ctx, tx, r.PathValue("id"), req.Child, true)
}
func (s *Server) handoff(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var req handoffRequest
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	return s.evaluateHandoff(ctx, tx, req.ParentID, req.Child, false)
}
func (s *Server) evaluateHandoff(ctx context.Context, tx pgx.Tx, id string, child envelope.Envelope, persist bool) (any, int, error) {
	parent, err := load(ctx, tx, id)
	if err != nil {
		return nil, 0, err
	}
	if child.ID == "" {
		child.ID = envelope.NewID()
	}
	if child.Version == "" {
		child.Version = "1"
	}
	if child.ParentEnvelope == "" {
		child.ParentEnvelope = id
	}
	now := time.Now()
	result := s.engine.Diff(parent.Envelope, child, now)
	if err = s.active(ctx, tx, parent, now); err != nil {
		result.Decision = "DENY"
		result.Violations = append(result.Violations, policy.Violation{Code: "PARENT_INACTIVE", Field: "parent_envelope", Message: err.Error()})
	}
	var childID any
	if result.Decision == "ALLOW" && persist {
		child.Signature, err = envelope.Sign(child, s.key)
		if err != nil {
			return nil, 0, err
		}
		if err = s.save(ctx, tx, parent.RunID, child); err != nil {
			return nil, 0, err
		}
		childID = child.ID
	}
	handoffID := newID("handoff_")
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO handoffs(id,run_id,parent_envelope_id,child_envelope_id,proposed_child_id,decision,result) VALUES($1,$2,$3,$4,$5,$6,$7)", handoffID, parent.RunID, id, childID, child.ID, result.Decision, raw); err != nil {
		return nil, 0, err
	}
	if err = recordViolations(ctx, tx, parent.RunID, id, result); err != nil {
		return nil, 0, err
	}
	payload := map[string]any{"id": handoffID, "parent_id": id, "proposed_child": child, "persisted": childID != nil, "result": result}
	if err = appendAudit(ctx, tx, parent.RunID, "handoff.evaluated", payload); err != nil {
		return nil, 0, err
	}
	response := map[string]any{"handoff_id": handoffID, "result": result}
	if childID != nil {
		response["envelope"] = child
	}
	if result.Decision == "DENY" {
		return response, 403, nil
	}
	if persist {
		return response, 201, nil
	}
	return response, 200, nil
}
func (s *Server) revoke(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	v, err := load(ctx, tx, r.PathValue("id"))
	if err != nil {
		return nil, 0, err
	}
	if v.RevokedAt == nil {
		if _, err = tx.Exec(ctx, "UPDATE envelopes SET revoked_at=now() WHERE id=$1", v.Envelope.ID); err != nil {
			return nil, 0, err
		}
		if err = appendAudit(ctx, tx, v.RunID, "envelope.revoked", map[string]string{"envelope_id": v.Envelope.ID}); err != nil {
			return nil, 0, err
		}
	}
	return map[string]string{"status": "revoked", "envelope_id": v.Envelope.ID}, 200, nil
}
func (s *Server) diff(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var req struct {
		Parent envelope.Envelope `json:"parent"`
		Child  envelope.Envelope `json:"child"`
	}
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	return map[string]any{"checks": "policy content only; signatures and revocation not checked", "result": s.engine.Diff(req.Parent, req.Child, time.Now())}, 200, nil
}
func recordViolations(ctx context.Context, tx pgx.Tx, run, id string, result policy.Result) error {
	for _, v := range result.Violations {
		if _, err := tx.Exec(ctx, "INSERT INTO violations(run_id,envelope_id,rule_id,field,description) VALUES($1,$2,$3,$4,$5)", run, id, v.Code, v.Field, v.Message); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) chain(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	run := r.PathValue("runId")
	if err := runExists(ctx, tx, run); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT document,revoked_at FROM envelopes WHERE run_id=$1 ORDER BY created_at,id", run)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []storedEnvelope{}
	for rows.Next() {
		v := storedEnvelope{RunID: run}
		var raw []byte
		if err := rows.Scan(&raw, &v.RevokedAt); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(raw, &v.Envelope); err != nil {
			return nil, 0, err
		}
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	handoffs, err := queryDocuments(ctx, tx, `SELECT jsonb_build_object('id',id,'parent_envelope_id',parent_envelope_id,'child_envelope_id',child_envelope_id,'proposed_child_id',proposed_child_id,'result',result,'created_at',created_at) FROM handoffs WHERE run_id=$1 ORDER BY created_at,id`, run)
	if err != nil {
		return nil, 0, err
	}
	actions, err := queryDocuments(ctx, tx, `SELECT jsonb_build_object('id',id,'envelope_id',envelope_id,'agent_id',agent_id,'tool',tool_name,'arguments_hash',arguments_hash,'result',result,'created_at',created_at) FROM tool_actions WHERE run_id=$1 ORDER BY created_at,id`, run)
	if err != nil {
		return nil, 0, err
	}
	return map[string]any{"run_id": run, "envelopes": result, "handoffs": handoffs, "actions": actions}, 200, nil
}
func (s *Server) violations(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	run := r.PathValue("runId")
	if err := runExists(ctx, tx, run); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT rule_id,field,description FROM violations WHERE run_id=$1 ORDER BY id", run)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []policy.Violation{}
	for rows.Next() {
		var v policy.Violation
		if err := rows.Scan(&v.Code, &v.Field, &v.Message); err != nil {
			return nil, 0, err
		}
		result = append(result, v)
	}
	return result, 200, rows.Err()
}
func runExists(ctx context.Context, tx pgx.Tx, run string) error {
	var id string
	return tx.QueryRow(ctx, "SELECT id FROM runs WHERE id=$1", run).Scan(&id)
}

func queryDocuments(ctx context.Context, tx pgx.Tx, query, run string) ([]json.RawMessage, error) {
	rows, err := tx.Query(ctx, query, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(raw))
	}
	return result, rows.Err()
}
