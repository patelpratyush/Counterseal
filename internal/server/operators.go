package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
	"handoffguard/internal/store"
)

type Operator struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}
type operatorKey struct{}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
var sessionPattern = regexp.MustCompile(`^op_[A-Za-z0-9_-]{43}$`)

func passwordHash(password string) (string, error) {
	if len(password) < 16 || len(password) > 256 {
		return "", fmt.Errorf("password must be 16–256 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	return "argon2id-v1:" + base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(key), nil
}
func passwordValid(password, encoded string) bool {
	parts := strings.Split(encoded, ":")
	salt, key := make([]byte, 16), make([]byte, 32)
	valid := false
	if len(parts) == 3 && parts[0] == "argon2id-v1" {
		var e1, e2 error
		salt, e1 = base64.RawStdEncoding.DecodeString(parts[1])
		key, e2 = base64.RawStdEncoding.DecodeString(parts[2])
		valid = e1 == nil && e2 == nil && len(salt) == 16 && len(key) == 32
	}
	if !valid {
		salt = make([]byte, 16)
		key = make([]byte, 32)
	}
	got := argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, 32)
	return subtle.ConstantTimeCompare(got, key) == 1 && valid
}

// ProvisionOperator is a host-administrator operation, never a service-token API.
func ProvisionOperator(ctx context.Context, db *store.Store, username, name, role, password string, ifAbsent bool) error {
	if !usernamePattern.MatchString(username) || strings.TrimSpace(name) == "" || len(name) > 100 || (role != "viewer" && role != "refund_manager") {
		return fmt.Errorf("valid username, display name, and viewer/refund_manager role required")
	}
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	return db.Transaction(ctx, func(tx pgx.Tx) error {
		if ifAbsent {
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM operators WHERE username=$1)", username).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return nil
			}
		}
		_, err := tx.Exec(ctx, "INSERT INTO operators(id,username,display_name,role,password_hash) VALUES($1,$2,$3,$4,$5)", newID("operator_"), username, name, role, hash)
		return err
	})
}
func ChangeOperator(ctx context.Context, db *store.Store, username, password string, disable bool) error {
	var hash string
	var err error
	if !disable {
		hash, err = passwordHash(password)
		if err != nil {
			return err
		}
	}
	return db.Transaction(ctx, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, "SELECT id FROM operators WHERE username=$1", username).Scan(&id); err != nil {
			return err
		}
		if disable {
			_, err = tx.Exec(ctx, "UPDATE operators SET disabled=true WHERE id=$1", id)
		} else {
			_, err = tx.Exec(ctx, "UPDATE operators SET password_hash=$2 WHERE id=$1", id, hash)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM operator_sessions WHERE operator_id=$1", id)
		return err
	})
}
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func currentOperator(ctx context.Context) *Operator {
	op, _ := ctx.Value(operatorKey{}).(*Operator)
	return op
}

func (s *Server) authenticateOperator(ctx context.Context, tx pgx.Tx, r *http.Request) (*Operator, error) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !sessionPattern.MatchString(token) {
		return nil, &apiError{401, "unauthorized"}
	}
	op := &Operator{}
	err := tx.QueryRow(ctx, `SELECT o.id,o.username,o.display_name,o.role FROM operators o JOIN operator_sessions s ON s.operator_id=o.id WHERE s.token_hash=$1 AND s.expires_at>now() AND NOT o.disabled`, tokenDigest(token)).Scan(&op.ID, &op.Username, &op.DisplayName, &op.Role)
	if err == pgx.ErrNoRows {
		return nil, &apiError{401, "unauthorized"}
	}
	return op, err
}
func operatorRoute(r *http.Request) bool {
	p := r.URL.Path
	if r.Method == "GET" {
		return p == "/v1/operators/me" || p == "/v1/dashboard/overview" || strings.HasPrefix(p, "/v1/runs/")
	}
	return r.Method == "POST" && (p == "/v1/operators/logout" || p == "/v1/approvals" || strings.HasPrefix(p, "/v1/audit/"))
}
func (s *Server) login(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		return nil, 0, err
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if len(req.Username) > 64 || len(req.Password) > 256 {
		return map[string]string{"error": "Invalid username or password"}, 401, nil
	}
	// Limits are durable and shared across API replicas. Do not trust forwarded IP headers.
	for _, bucket := range []string{"global", "user:" + req.Username} {
		var count int
		err := tx.QueryRow(ctx, `INSERT INTO login_limits(bucket,attempts,reset_at) VALUES($1,1,now()+interval '1 minute') ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN login_limits.reset_at<=now() THEN 1 ELSE login_limits.attempts+1 END, reset_at=CASE WHEN login_limits.reset_at<=now() THEN now()+interval '1 minute' ELSE login_limits.reset_at END RETURNING attempts`, bucket).Scan(&count)
		if err != nil {
			return nil, 0, err
		}
		limit := 10
		if bucket == "global" {
			limit = 60
		}
		if count > limit {
			return map[string]string{"error": "Too many attempts. Please wait a minute."}, 429, nil
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM login_limits WHERE reset_at<now()-interval '1 hour'"); err != nil {
		return nil, 0, err
	}
	var op Operator
	var hash string
	var disabled bool
	err := tx.QueryRow(ctx, "SELECT id,username,display_name,role,password_hash,disabled FROM operators WHERE username=$1", req.Username).Scan(&op.ID, &op.Username, &op.DisplayName, &op.Role, &hash, &disabled)
	if err != nil && err != pgx.ErrNoRows {
		return nil, 0, err
	}
	valid := passwordValid(req.Password, hash)
	if !valid || disabled || err == pgx.ErrNoRows {
		return map[string]string{"error": "Invalid username or password"}, 401, nil
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return nil, 0, err
	}
	token := "op_" + base64.RawURLEncoding.EncodeToString(random)
	expires := time.Now().Add(8 * time.Hour)
	if _, err = tx.Exec(ctx, "DELETE FROM operator_sessions WHERE expires_at<=now()"); err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO operator_sessions(token_hash,operator_id,expires_at) VALUES($1,$2,$3)", tokenDigest(token), op.ID, expires); err != nil {
		return nil, 0, err
	}
	return map[string]any{"token": token, "operator": op, "expires_at": expires}, 200, nil
}
func (s *Server) me(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	op := currentOperator(ctx)
	if op == nil {
		return nil, 0, denied("operator session required")
	}
	return op, 200, nil
}
func (s *Server) logout(ctx context.Context, tx pgx.Tx, r *http.Request) (any, int, error) {
	if currentOperator(ctx) == nil {
		return nil, 0, denied("operator session required")
	}
	_, err := tx.Exec(ctx, "DELETE FROM operator_sessions WHERE token_hash=$1", tokenDigest(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	return map[string]string{"status": "signed_out"}, 200, err
}
