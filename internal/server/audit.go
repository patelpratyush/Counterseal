package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"handoffguard/internal/envelope"
)

type auditHeader struct {
	ID          string    `json:"id"`
	RunID       string    `json:"run_id"`
	Type        string    `json:"event_type"`
	PayloadHash string    `json:"payload_hash"`
	Previous    string    `json:"previous_event_hash"`
	Timestamp   time.Time `json:"timestamp"`
}

func appendAudit(ctx context.Context, tx pgx.Tx, run, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Normalize through the same representation used when reading JSONB.
	var normalized any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err = d.Decode(&normalized); err != nil {
		return err
	}
	payloadHash, err := digest(normalized)
	if err != nil {
		return err
	}
	previous := ""
	err = tx.QueryRow(ctx, "SELECT event_hash FROM audit_events WHERE run_id=$1 ORDER BY seq DESC LIMIT 1", run).Scan(&previous)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	header := auditHeader{ID: newID("evt_"), RunID: run, Type: kind, PayloadHash: payloadHash, Previous: previous, Timestamp: time.Now().UTC().Truncate(time.Microsecond)}
	hash, err := digest(header)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO audit_events(id,run_id,event_type,payload,payload_hash,previous_event_hash,event_hash,timestamp) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", header.ID, run, kind, raw, payloadHash, previous, hash, header.Timestamp)
	return err
}
func (s *Server) verifyAudit(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	run := r.PathValue("runId")
	if err := runExists(ctx, tx, run); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, "SELECT id,run_id,event_type,payload,payload_hash,previous_event_hash,event_hash,timestamp FROM audit_events WHERE run_id=$1 ORDER BY seq", run)
	if err != nil {
		return nil, 0, err
	}
	failures := []string{}
	previous := ""
	count := 0
	snapshots := map[string]string{}
	for rows.Next() {
		var h auditHeader
		var raw []byte
		var hash string
		if err = rows.Scan(&h.ID, &h.RunID, &h.Type, &raw, &h.PayloadHash, &h.Previous, &hash, &h.Timestamp); err != nil {
			rows.Close()
			return nil, 0, err
		}
		h.Timestamp = h.Timestamp.UTC()
		var payload any
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err = d.Decode(&payload); err != nil {
			rows.Close()
			return nil, 0, err
		}
		payloadHash, err := digest(payload)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		expected, err := digest(h)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		if h.Previous != previous || expected != hash || payloadHash != h.PayloadHash {
			failures = append(failures, "audit event modified: "+h.ID)
		}
		// Bind persisted envelopes to the signed snapshot recorded at creation.
		object, _ := payload.(map[string]any)
		var snapshot map[string]any
		if h.Type == "envelope.created" {
			snapshot = object
		}
		if h.Type == "handoff.evaluated" && object["persisted"] == true {
			snapshot, _ = object["proposed_child"].(map[string]any)
		}
		if snapshot != nil {
			id, _ := snapshot["id"].(string)
			signature, _ := snapshot["signature"].(string)
			if id == "" || signature == "" {
				failures = append(failures, "invalid envelope snapshot: "+h.ID)
			} else {
				snapshots[id] = signature
			}
		}
		previous = hash
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if count == 0 {
		failures = append(failures, "missing audit events")
	}
	rows, err = tx.Query(ctx, "SELECT document,parent_id,id FROM envelopes WHERE run_id=$1 ORDER BY id", run)
	if err != nil {
		return nil, 0, err
	}
	envelopes := map[string]envelope.Envelope{}
	parents := map[string]string{}
	for rows.Next() {
		var raw []byte
		var parent *string
		var id string
		if err = rows.Scan(&raw, &parent, &id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		var v envelope.Envelope
		if err = json.Unmarshal(raw, &v); err != nil {
			rows.Close()
			return nil, 0, err
		}
		envelopes[id] = v
		if parent != nil {
			parents[id] = *parent
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	for id, signature := range snapshots {
		if v, ok := envelopes[id]; !ok || v.Signature != signature {
			failures = append(failures, "missing or replaced envelope: "+id)
		}
	}
	for id, v := range envelopes {
		if _, ok := snapshots[id]; !ok {
			failures = append(failures, "missing creation event: "+id)
		}
		if err = envelope.Verify(v, s.pub); err != nil || v.ID != id {
			failures = append(failures, "envelope signature invalid: "+id)
		}
		if parents[id] != v.ParentEnvelope {
			failures = append(failures, "parent reference modified: "+id)
		}
		if parent := v.ParentEnvelope; parent != "" {
			if _, ok := envelopes[parent]; !ok {
				failures = append(failures, "missing parent: "+id)
			}
		}
	}
	sort.Strings(failures)
	status := "VALID"
	if len(failures) > 0 {
		status = "INVALID"
	}
	return map[string]any{"status": status, "events_checked": count, "envelopes_checked": len(envelopes), "head_hash": previous, "failures": failures}, 200, nil
}
