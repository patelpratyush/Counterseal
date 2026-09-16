// Package policy checks monotonic delegation. Diff compares policy content;
// it does not authenticate signatures, resolve parents, or check revocation.
package policy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"handoffguard/internal/envelope"
)

type Violation struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Result struct {
	Decision   string      `json:"decision"`
	Violations []Violation `json:"violations"`
}

type Engine struct{ conditions *Conditions }

func New() (*Engine, error) {
	c, err := NewConditions()
	if err != nil {
		return nil, err
	}
	return &Engine{conditions: c}, nil
}

// Diff is deterministic for the same envelopes and explicit evaluation time.
// Empty permission sets grant nothing. Resource scopes accept exact strings,
// '*' (all resources in that category), and terminal '/*' prefix patterns.
// Actions and data classes are exact identifiers; wildcards there are invalid.
func (e *Engine) Diff(parent, child envelope.Envelope, now time.Time) Result {
	result := Result{Decision: "ALLOW", Violations: []Violation{}}
	add := func(code, field, message string) {
		result.Violations = append(result.Violations, Violation{code, field, message})
	}
	e.validate(parent, "parent", now, add)
	e.validate(child, "child", now, add)
	if len(result.Violations) != 0 {
		return finish(result)
	}
	if child.ParentEnvelope != parent.ID {
		add("PARENT_MISMATCH", "parent_envelope", "child must reference the parent id")
	}
	if child.ID == parent.ID {
		add("ID_REUSED", "id", "child must have a different id")
	}
	if child.Issuer != parent.Recipient {
		add("ISSUER_MISMATCH", "issuer", "child issuer must match parent recipient")
	}
	if child.Purpose != parent.Purpose {
		add("PURPOSE_CHANGED", "purpose", "child must retain parent purpose")
	}
	if child.PolicyVersion != parent.PolicyVersion {
		add("POLICY_VERSION_CHANGED", "policy_version", "child must retain policy version")
	}
	if child.ExpiresAt.After(parent.ExpiresAt) {
		add("EXPIRY_EXPANDED", "expires_at", "child expires after parent")
	}
	if child.Delegation.MaxDepth > parent.Delegation.MaxDepth {
		add("DEPTH_EXPANDED", "delegation.max_depth", "child increases maximum depth")
	}
	// Subtraction avoids overflow at the maximum representable integer.
	if child.Delegation.CurrentDepth == 0 || child.Delegation.CurrentDepth-1 != parent.Delegation.CurrentDepth {
		add("DEPTH_INVALID", "delegation.current_depth", "child depth must equal parent depth plus one")
	}
	for _, action := range unique(child.AllowedActions) {
		if !contains(parent.AllowedActions, action) {
			add("ACTION_EXPANDED", "allowed_actions", fmt.Sprintf("new action %q", action))
		}
	}
	for _, action := range unique(parent.DeniedActions) {
		if !contains(child.DeniedActions, action) {
			add("DENIAL_REMOVED", "denied_actions", fmt.Sprintf("removed denial %q", action))
		}
	}
	for _, data := range unique(child.DataClasses) {
		if !contains(parent.DataClasses, data) {
			add("DATA_EXPANDED", "data_classes", fmt.Sprintf("new data class %q", data))
		}
	}
	for category, scopes := range child.Resources {
		for _, scope := range unique(scopes) {
			covered := false
			for _, allowed := range parent.Resources[category] {
				if covers(allowed, scope) {
					covered = true
					break
				}
			}
			if !covered {
				add("RESOURCE_EXPANDED", "resources."+category, fmt.Sprintf("scope %q is outside parent resources", scope))
			}
		}
	}
	for _, requirement := range parent.Approvals {
		p, _ := e.conditions.compile(requirement.Condition)
		inherited := false
		for _, proposed := range child.Approvals {
			if proposed.RequiredRole != requirement.RequiredRole {
				continue
			}
			c, _ := e.conditions.compile(proposed.Condition)
			if preserves(p, c) {
				inherited = true
				break
			}
		}
		if !inherited {
			add("APPROVAL_WEAKENED", "approvals", fmt.Sprintf("cannot prove inheritance of role %q when %s", requirement.RequiredRole, requirement.Condition))
		}
	}
	return finish(result)
}

func (e *Engine) validate(v envelope.Envelope, side string, now time.Time, add func(string, string, string)) {
	invalid := func(field, reason string) { add("INVALID_ENVELOPE", side+"."+field, reason) }
	if err := envelope.ValidateStructure(v); err != nil {
		invalid("structure", err.Error())
	}
	if err := envelope.ValidateExpiration(v, now); err != nil {
		invalid("expires_at", err.Error())
	}
	if v.Version != "1" {
		invalid("version", "only envelope version 1 is supported")
	}
	for field, value := range map[string]string{"id": v.ID, "issuer.agent": v.Issuer.Agent, "recipient.agent": v.Recipient.Agent, "purpose": v.Purpose, "policy_version": v.PolicyVersion} {
		if strings.TrimSpace(value) == "" {
			invalid(field, "must not be blank")
		}
	}
	if v.Delegation.MaxDepth < 0 || v.Delegation.CurrentDepth < 0 || v.Delegation.CurrentDepth > v.Delegation.MaxDepth {
		invalid("delegation", "depth must be nonnegative and within maximum")
	}
	if v.Delegation.MayExpandAuthority {
		invalid("delegation.may_expand_authority", "authority expansion is not supported")
	}
	for field, values := range map[string][]string{"allowed_actions": v.AllowedActions, "denied_actions": v.DeniedActions, "data_classes": v.DataClasses} {
		for _, value := range unique(values) {
			if strings.TrimSpace(value) != value || value == "" || strings.ContainsAny(value, "*?[]") {
				invalid(field, "expected nonblank exact identifiers without wildcards")
			}
		}
	}
	for category, scopes := range v.Resources {
		if category == "" || strings.TrimSpace(category) != category || strings.ContainsAny(category, "*?[]") {
			invalid("resources", "invalid resource category")
		}
		for _, scope := range unique(scopes) {
			if !validScope(scope) {
				invalid("resources."+category, fmt.Sprintf("unsupported resource pattern %q", scope))
			}
		}
	}
	for _, approval := range v.Approvals {
		if strings.TrimSpace(approval.RequiredRole) == "" {
			invalid("approvals", "required_role must not be blank")
		}
		if _, err := e.conditions.compile(approval.Condition); err != nil {
			invalid("approvals", fmt.Sprintf("invalid condition %q: %v", approval.Condition, err))
		}
	}
}

// Validate checks a standalone policy envelope, including CEL compilation.
func (e *Engine) Validate(v envelope.Envelope, now time.Time) Result {
	r := Result{Decision: "ALLOW", Violations: []Violation{}}
	e.validate(v, "envelope", now, func(code, field, message string) {
		r.Violations = append(r.Violations, Violation{code, field, message})
	})
	return finish(r)
}

func finish(r Result) Result {
	sort.Slice(r.Violations, func(i, j int) bool {
		a, b := r.Violations[i], r.Violations[j]
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	if len(r.Violations) != 0 {
		r.Decision = "DENY"
	}
	return r
}

func validScope(s string) bool {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsAny(s, "?[]") {
		return false
	}
	return !strings.Contains(s, "*") || s == "*" || (strings.HasSuffix(s, "/*") && strings.Count(s, "*") == 1)
}
func covers(parent, child string) bool {
	if parent == "*" || parent == child {
		return true
	}
	return strings.HasSuffix(parent, "/*") && strings.HasPrefix(child, strings.TrimSuffix(parent, "*"))
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func unique(values []string) []string {
	set := map[string]bool{}
	for _, v := range values {
		set[v] = true
	}
	result := make([]string, 0, len(set))
	for v := range set {
		result = append(result, v)
	}
	sort.Strings(result)
	return result
}
