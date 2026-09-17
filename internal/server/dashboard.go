package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// overview supplies bounded, searchable run summaries without transferring envelopes.
func (s *Server) overview(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 100000 {
			return nil, 0, bad("page must be between 1 and 100000")
		}
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 100 {
		return nil, 0, bad("search must be at most 100 bytes")
	}
	filter := r.URL.Query().Get("filter")
	if filter != "" && filter != "blocked" {
		return nil, 0, bad("filter must be blocked or empty")
	}
	var runs, handoffs, blockedHandoffs, blockedActions, versions int64
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM runs),
 (SELECT count(*) FROM handoffs), (SELECT count(*) FROM handoffs WHERE decision='DENY'),
 (SELECT count(*) FROM tool_actions WHERE decision='DENY'),
 (SELECT count(DISTINCT document->>'policy_version') FROM envelopes)`).Scan(&runs, &handoffs, &blockedHandoffs, &blockedActions, &versions)
	if err != nil {
		return nil, 0, err
	}
	// Literal substring matching avoids treating user-entered percent/underscore as wildcards.
	const source = ` FROM runs r JOIN envelopes e ON e.run_id=r.id AND e.parent_id IS NULL
 WHERE (strpos(lower(r.id),lower($1))>0 OR strpos(lower(e.document->>'purpose'),lower($1))>0)
 AND ($2='' OR EXISTS(SELECT 1 FROM handoffs h WHERE h.run_id=r.id AND h.decision='DENY')
 OR EXISTS(SELECT 1 FROM tool_actions a WHERE a.run_id=r.id AND a.decision='DENY'))`
	var total int64
	if err := tx.QueryRow(ctx, "SELECT count(*)"+source, query, filter).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT jsonb_build_object('id',r.id,'created_at',r.created_at,
 'purpose',e.document->>'purpose','agent',e.document->'recipient'->>'agent',
 'policy_version',e.document->>'policy_version',
 'envelopes',(SELECT count(*) FROM envelopes x WHERE x.run_id=r.id),
 'handoffs',(SELECT count(*) FROM handoffs h WHERE h.run_id=r.id),
 'blocked',(SELECT count(*) FROM handoffs h WHERE h.run_id=r.id AND h.decision='DENY')+
 (SELECT count(*) FROM tool_actions a WHERE a.run_id=r.id AND a.decision='DENY'))`+source+` ORDER BY r.created_at DESC,r.id DESC LIMIT 20 OFFSET $3`, query, filter, (page-1)*20)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	summaries := make([]map[string]any, 0)
	for rows.Next() {
		var value map[string]any
		if err := rows.Scan(&value); err != nil {
			return nil, 0, err
		}
		summaries = append(summaries, value)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return map[string]any{"stats": map[string]int64{"runs": runs, "handoffs": handoffs, "blocked_handoffs": blockedHandoffs, "blocked_actions": blockedActions, "policy_versions": versions}, "runs": summaries, "total": total, "page": page, "page_size": 20}, 200, nil
}
