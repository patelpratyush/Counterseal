// Package server provides the trusted, single-tenant HandoffGuard control API.
package server

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"handoffguard/internal/envelope"
	"handoffguard/internal/policy"
	"handoffguard/internal/store"
)

type Server struct {
	db         *store.Store
	key        ed25519.PrivateKey
	pub        ed25519.PublicKey
	tokenHash  [32]byte
	engine     *policy.Engine
	conditions *policy.Conditions
}

func New(ctx context.Context, db *store.Store, key ed25519.PrivateKey, token string) (*Server, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid server signing key")
	}
	if subtle.ConstantTimeCompare(key, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])) != 1 {
		return nil, fmt.Errorf("inconsistent server private key")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("server token must be at least 32 bytes")
	}
	engine, err := policy.New()
	if err != nil {
		return nil, err
	}
	conditions, err := policy.NewConditions()
	if err != nil {
		return nil, err
	}
	s := &Server{db: db, key: append(ed25519.PrivateKey(nil), key...), pub: key.Public().(ed25519.PublicKey), tokenHash: sha256.Sum256([]byte(token)), engine: engine, conditions: conditions}
	// Refuse accidental key rotation against an existing database.
	err = db.Transaction(ctx, func(tx pgx.Tx) error {
		value := hex.EncodeToString(s.pub)
		if _, err := tx.Exec(ctx, "INSERT INTO server_metadata(name,value) VALUES('signing_key',$1) ON CONFLICT DO NOTHING", value); err != nil {
			return err
		}
		var saved string
		if err := tx.QueryRow(ctx, "SELECT value FROM server_metadata WHERE name='signing_key'").Scan(&saved); err != nil {
			return err
		}
		if saved != value {
			return fmt.Errorf("signing key does not match database; key rotation requires explicit migration")
		}
		return nil
	})
	return s, err
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }
func bad(message string) error    { return &apiError{http.StatusBadRequest, message} }
func denied(message string) error { return &apiError{http.StatusForbidden, message} }

type endpoint func(context.Context, pgx.Tx, *http.Request) (any, int, error)

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.db.Pool.Ping(ctx); err != nil {
			writeJSON(w, 503, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/envelopes", s.wrap(s.create))
	mux.HandleFunc("GET /v1/envelopes/{id}", s.wrap(s.get))
	mux.HandleFunc("POST /v1/envelopes/{id}/delegate", s.wrap(s.delegate))
	mux.HandleFunc("POST /v1/envelopes/{id}/revoke", s.wrap(s.revoke))
	mux.HandleFunc("POST /v1/evaluate/handoff", s.wrap(s.handoff))
	mux.HandleFunc("POST /v1/evaluate/action", s.wrap(s.action))
	mux.HandleFunc("POST /v1/approvals", s.wrap(s.approve))
	mux.HandleFunc("GET /v1/runs/{runId}/chain", s.wrap(s.chain))
	mux.HandleFunc("GET /v1/runs/{runId}/violations", s.wrap(s.violations))
	mux.HandleFunc("POST /v1/policies/diff", s.wrap(s.diff))
	mux.HandleFunc("POST /v1/audit/{runId}/verify", s.wrap(s.verifyAudit))
	return mux
}

func (s *Server) wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		hash := sha256.Sum256([]byte(token))
		if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) != 1 {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var response any
		var status int
		err := s.db.Transaction(ctx, func(tx pgx.Tx) error { var err error; response, status, err = fn(ctx, tx, r); return err })
		if err != nil {
			var api *apiError
			var pg *pgconn.PgError
			switch {
			case errors.As(err, &api):
				writeJSON(w, api.status, map[string]string{"error": api.message})
			case errors.Is(err, pgx.ErrNoRows):
				writeJSON(w, 404, map[string]string{"error": "not found"})
			case errors.As(err, &pg) && pg.Code == "23505":
				writeJSON(w, 409, map[string]string{"error": "record already exists"})
			default:
				slog.Error("API transaction failed", "method", r.Method, "path", r.URL.Path, "error", err)
				writeJSON(w, 500, map[string]string{"error": "internal server error"})
			}
			return
		}
		writeJSON(w, status, response)
	}
}

func decode(r *http.Request, value any) error {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return bad("could not read request body (maximum 1 MiB)")
	}
	// Duplicate fields are rejected before typed decoding; Go's normal decoder
	// would silently replace earlier authority constraints.
	if err = uniqueJSON(raw); err != nil {
		return bad("invalid JSON: " + err.Error())
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err = d.Decode(value); err != nil {
		return bad("invalid JSON: " + err.Error())
	}
	return nil
}
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k := strings.ToLower(key.(string))
				if seen[k] {
					return fmt.Errorf("duplicate key %q", k)
				}
				seen[k] = true
			}
			if err := walk(); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("expected one document")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func newID(prefix string) string { return prefix + strings.TrimPrefix(envelope.NewID(), "env_") }
func digest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
