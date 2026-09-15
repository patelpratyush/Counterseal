package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Party identifies an agent by name and optional version, used for both
// the issuer and recipient of an envelope.
type Party struct {
	Agent   string `json:"agent"`
	Version string `json:"version,omitempty"`
}

// Approval describes a condition under which human sign-off is required
// before an action may proceed.
type Approval struct {
	Condition    string `json:"condition"`
	RequiredRole string `json:"required_role"`
}

// Delegation constrains how far and how loosely an envelope may be
// re-delegated to a further child.
type Delegation struct {
	MaxDepth           int  `json:"max_depth"`
	CurrentDepth       int  `json:"current_depth"`
	MayExpandAuthority bool `json:"may_expand_authority"`
}

// Envelope is a signed, tamper-evident record of delegated authority.
// Field order and JSON tags match PRD §7.
type Envelope struct {
	ID             string              `json:"id"`
	Version        string              `json:"version"`
	Issuer         Party               `json:"issuer"`
	Recipient      Party               `json:"recipient"`
	Purpose        string              `json:"purpose"`
	Resources      map[string][]string `json:"resources"`
	AllowedActions []string            `json:"allowed_actions"`
	DeniedActions  []string            `json:"denied_actions"`
	DataClasses    []string            `json:"data_classes"`
	Approvals      []Approval          `json:"approvals"`
	Delegation     Delegation          `json:"delegation"`
	ExpiresAt      time.Time           `json:"expires_at"`
	PolicyVersion  string              `json:"policy_version"`
	ParentEnvelope string              `json:"parent_envelope,omitempty"`
	Signature      string              `json:"signature,omitempty"`
}

// NewID generates a random envelope identifier prefixed "env_".
func NewID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "env_" + hex.EncodeToString(b)
}
