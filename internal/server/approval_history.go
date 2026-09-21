package server

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

func (s *Server) listApprovals(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var run string
	if err := tx.QueryRow(ctx, "SELECT id FROM runs WHERE id=$1", r.PathValue("runId")).Scan(&run); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT a.id,a.envelope_id,a.resource,a.approver_id,coalesce(o.display_name,a.approver_id),a.approver_role,a.operator_id,a.refund_amount,a.created_at,a.expires_at,a.consumed_at FROM approvals a JOIN envelopes e ON e.id=a.envelope_id LEFT JOIN operators o ON o.id=a.operator_id WHERE e.run_id=$1 ORDER BY a.created_at DESC,a.id LIMIT 100`, run)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	type entry struct {
		ID         string     `json:"id"`
		EnvelopeID string     `json:"envelope_id"`
		Resource   string     `json:"resource"`
		Username   string     `json:"username"`
		Name       string     `json:"display_name"`
		Role       string     `json:"role"`
		OperatorID *string    `json:"operator_id"`
		Amount     *int64     `json:"amount"`
		CreatedAt  time.Time  `json:"created_at"`
		ExpiresAt  time.Time  `json:"expires_at"`
		ConsumedAt *time.Time `json:"consumed_at"`
	}
	result := []entry{}
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.ID, &v.EnvelopeID, &v.Resource, &v.Username, &v.Name, &v.Role, &v.OperatorID, &v.Amount, &v.CreatedAt, &v.ExpiresAt, &v.ConsumedAt); err != nil {
			return nil, 0, err
		}
		result = append(result, v)
	}
	return map[string]any{"approvals": result, "as_of": time.Now()}, 200, rows.Err()
}
