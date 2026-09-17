package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CommandTransport launches an explicitly configured upstream executable
// without a shell. Only a small environment allowlist is inherited.
func CommandTransport(ctx context.Context, args, extraEnv []string, stderr io.Writer) (mcp.Transport, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("an upstream command is required")
	}
	env, err := UpstreamEnvironment(extraEnv)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	cmd.Stderr = stderr
	return &mcp.CommandTransport{Command: cmd, TerminateDuration: 2 * time.Second}, nil
}
func UpstreamEnvironment(extra []string) ([]string, error) {
	names := map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true, "TEMP": true, "TMP": true, "LANG": true, "LC_ALL": true, "SYSTEMROOT": true, "USERPROFILE": true}
	for _, name := range extra {
		if name == "" || strings.ContainsAny(name, "=\x00") || strings.HasPrefix(strings.ToUpper(name), "HANDOFFGUARD_") {
			return nil, fmt.Errorf("invalid or reserved upstream environment variable")
		}
		names[name] = true
	}
	result := []string{}
	for name := range names {
		if value, ok := os.LookupEnv(name); ok {
			result = append(result, name+"="+value)
		}
	}
	sort.Strings(result)
	return result, nil
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Del("Authorization")
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}
func HTTPTransport(endpoint, token string) (mcp.Transport, error) {
	if err := ValidateURL(endpoint); err != nil {
		return nil, err
	}
	client := &http.Client{Transport: bearerTransport{token: token, base: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true, MaxEventSize: 4 << 20}, nil
}
func NewUpstreamClient() *mcp.Client {
	// Retrying a side effect after an input-required result would bypass a new
	// authorization/approval consumption. Never enable SDK auto-continuations.
	return mcp.NewClient(&mcp.Implementation{Name: "handoffguard-gateway", Version: "0.1.0"}, &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
}
