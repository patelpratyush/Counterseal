package envelope

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	e := Envelope{
		ID:      "env_test",
		Version: "1",
		Issuer:  Party{Agent: "support-agent", Version: "2.3"},
		Recipient: Party{Agent: "billing-agent"},
		Purpose: "customer_support_refund",
		Resources: map[string][]string{
			"orders":    {"48319"},
			"customers": {"cus_8291"},
		},
		AllowedActions: []string{"orders.read", "refunds.create"},
		DeniedActions:  []string{"customers.delete"},
		DataClasses:    []string{"customer_pii"},
		Approvals: []Approval{
			{Condition: "refund.amount > 500", RequiredRole: "refund_manager"},
		},
		Delegation: Delegation{MaxDepth: 2, CurrentDepth: 1, MayExpandAuthority: false},
		ExpiresAt:      time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC),
		PolicyVersion:  "refund-policy-v8",
		ParentEnvelope: "env_parent",
		Signature:      "sig123",
	}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Envelope
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != e.ID || got.Issuer.Agent != e.Issuer.Agent || got.Delegation.MaxDepth != e.Delegation.MaxDepth {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, e)
	}
}

func TestNewIDHasPrefixAndIsUnique(t *testing.T) {
	a := NewID()
	b := NewID()
	if !strings.HasPrefix(a, "env_") {
		t.Fatalf("expected env_ prefix, got %q", a)
	}
	if a == b {
		t.Fatalf("expected unique ids, got two equal: %q", a)
	}
}
