// Package gateway exposes explicitly mapped upstream MCP tools only after a
// committed authorization decision from the Counterseal control API.
package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"handoffguard/internal/strictjson"
)

type Upstream interface {
	ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	CallTool(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
}
type Options struct {
	AgentID, EnvelopeID string
	Timeout             time.Duration
	Logger              *slog.Logger
}

func New(ctx context.Context, upstream Upstream, authorizer Authorizer, config Config, options Options) (*mcp.Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !identifier(options.AgentID) || !identifier(options.EnvelopeID) {
		return nil, fmt.Errorf("a fixed agent and envelope are required")
	}
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}
	if options.Timeout < 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "handoffguard-gateway", Version: "0.1.0"}, &mcp.ServerOptions{Logger: options.Logger, Capabilities: &mcp.ServerCapabilities{}, PageSize: 50, SupportedProtocolVersions: []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}})
	discovery, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cursor := ""
	seenCursors := map[string]bool{}
	found := map[string]bool{}
	seenTools := map[string]bool{}
	total := 0
	for {
		page, err := upstream.ListTools(discovery, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("upstream discovery failed: %w", err)
		}
		if page == nil {
			return nil, fmt.Errorf("upstream returned an empty discovery response")
		}
		for _, tool := range page.Tools {
			total++
			if total > 4096 {
				return nil, fmt.Errorf("upstream tool count exceeds 4096")
			}
			if tool == nil || tool.Name == "" || seenTools[tool.Name] {
				return nil, fmt.Errorf("invalid or duplicate upstream tool")
			}
			seenTools[tool.Name] = true
			mapping, ok := config.Tools[tool.Name]
			if !ok {
				continue
			}
			// Copy trusted mappings so callers cannot mutate the authorization policy.
			encoded, err := json.Marshal(mapping)
			if err != nil {
				return nil, err
			}
			var pinned Mapping
			if err = json.Unmarshal(encoded, &pinned); err != nil {
				return nil, err
			}
			raw, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, err
			}
			resolved, err := compileSchema(raw)
			if err != nil {
				return nil, fmt.Errorf("unsupported input schema for %s: %w", tool.Name, err)
			}

			// No metadata, task extensions, or parameter-header annotations are
			// propagated. Only tools/call is exposed; all other proxy features are absent.
			exposed := &mcp.Tool{Name: tool.Name, Title: tool.Title, Description: tool.Description, InputSchema: json.RawMessage(raw), OutputSchema: tool.OutputSchema}
			server.AddTool(exposed, handler(upstream, authorizer, tool.Name, pinned, resolved, options))
			found[tool.Name] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
		if seenCursors[cursor] || len(seenCursors) >= 100 {
			return nil, fmt.Errorf("invalid upstream pagination")
		}
		seenCursors[cursor] = true
	}
	for name := range config.Tools {
		if !found[name] {
			return nil, fmt.Errorf("configured tool %s was not discovered", name)
		}
	}
	return server, nil
}

func handler(upstream Upstream, authorizer Authorizer, name string, mapping Mapping, schema *jsonschema.Schema, options Options) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(ctx, options.Timeout)
		defer cancel()
		log := options.Logger.With("tool", name, "agent_id", options.AgentID, "envelope_id", options.EnvelopeID)
		reject := func(code, message string) (*mcp.CallToolResult, error) {
			log.Warn("gateway blocked call", "code", code)
			return toolError(code + ": " + message), nil
		}
		if req.Params.RequestState != "" || len(req.Params.InputResponses) > 0 {
			return reject("UNSUPPORTED_CONTINUATION", "Multi-round-trip calls are not supported.")
		}
		raw := req.Params.Arguments
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		if len(raw) > 256<<10 {
			return reject("INVALID_ARGUMENTS", "Arguments exceed 256 KiB.")
		}
		var arguments map[string]any
		if err := strictjson.Decode(raw, &arguments); err != nil || arguments == nil {
			return reject("INVALID_ARGUMENTS", "Provide one JSON object without duplicate keys.")
		}
		if !boundedNumbers(arguments) {
			return reject("INVALID_ARGUMENTS", "Numeric literals exceed supported precision or exponent limits.")
		}
		if err := schema.Validate(arguments); err != nil {
			return reject("INVALID_ARGUMENTS", "Arguments do not match the advertised tool schema.")
		}
		resources, err := extract(arguments, mapping)
		if err != nil {
			return reject("RESOURCE_MAPPING_FAILED", err.Error())
		}
		// The same decoded object is hashed/checked by the API and forwarded;
		// numbers are never converted through float64 and defaults are not applied.
		action := Action{EnvelopeID: options.EnvelopeID, AgentID: options.AgentID, Tool: mapping.Action, Resources: resources, DataClasses: mapping.DataClasses, Arguments: arguments}
		decision, err := authorizer.Authorize(ctx, action)
		if err != nil {
			return reject("AUTHORIZATION_UNAVAILABLE", "No tool was forwarded. Ask the operator to check the authorization service; an approval may have been consumed if its response was lost.")
		}
		log = log.With("action_id", decision.ActionID)
		if decision.Result.Decision != "ALLOW" || len(decision.Result.Violations) > 0 || decision.ActionID == "" {
			codes := []string{}
			for _, v := range decision.Result.Violations {
				codes = append(codes, v.Code)
			}
			sort.Strings(codes)
			return reject("AUTHORIZATION_DENIED", strings.Join(codes, ", ")+". Request the required scope or approval from the operator.")
		}
		if ctx.Err() != nil {
			return reject("AUTHORIZATION_EXPIRED", "Authorization completed after the call deadline. No tool was forwarded; an approval may have been consumed.")
		}
		log.Info("gateway authorized call")
		result, err := upstream.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil || result == nil {
			log.Error("gateway upstream outcome unknown")
			return toolError("UPSTREAM_OUTCOME_UNKNOWN: The call was authorized but its result was not received. It may have executed; reconcile action " + decision.ActionID + " before retrying."), nil
		}
		if result.NeedsInput() || result.InputRequests != nil || result.RequestState != "" {
			log.Error("gateway unsupported upstream continuation")
			return toolError("UPSTREAM_OUTCOME_UNKNOWN: Interactive continuation is unsupported; reconcile action " + decision.ActionID + " before retrying."), nil
		}
		// Log only an outcome and result hash, never tool arguments or result data.
		encoded, err := json.Marshal(result)
		if err != nil {
			log.Error("gateway invalid upstream result")
			return toolError("UPSTREAM_OUTCOME_UNKNOWN: Invalid upstream result; reconcile action " + decision.ActionID + " before retrying."), nil
		}
		sum := sha256.Sum256(encoded)
		log.Info("gateway tool completed", "is_error", result.IsError, "result_hash", hex.EncodeToString(sum[:]))
		return result, nil
	}
}
func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

// Reject external references: tool discovery must not trigger filesystem reads
// or network requests. Local references within the schema remain supported.
type denySchemaLoader struct{}

func (denySchemaLoader) Load(string) (any, error) {
	return nil, fmt.Errorf("external schema references are unsupported")
}
func compileSchema(raw []byte) (*jsonschema.Schema, error) {
	if len(raw) > 256<<10 {
		return nil, fmt.Errorf("tool schema exceeds 256 KiB")
	}
	var document map[string]any
	if err := strictjson.Decode(raw, &document); err != nil {
		return nil, err
	}
	if !boundedNumbers(document) {
		return nil, fmt.Errorf("schema numeric limits exceeded")
	}
	if document["type"] != "object" {
		return nil, fmt.Errorf("tool input schema must be an object")
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denySchemaLoader{})
	const location = "https://handoffguard.invalid/tool-schema.json"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

// Bound rational-number work before schema validation. Very large exponents
// can otherwise allocate disproportionate memory even in tiny JSON requests.
func boundedNumbers(value any) bool {
	switch v := value.(type) {
	case json.Number:
		if len(v) > 128 {
			return false
		}
		if index := strings.IndexAny(string(v), "eE"); index >= 0 {
			exponent, err := strconv.ParseInt(string(v)[index+1:], 10, 32)
			if err != nil || exponent < -308 || exponent > 308 {
				return false
			}
		}
	case map[string]any:
		for _, child := range v {
			if !boundedNumbers(child) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !boundedNumbers(child) {
				return false
			}
		}
	}
	return true
}
