package policy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"handoffguard/internal/envelope"
)

var testNow = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

func pair() (envelope.Envelope, envelope.Envelope) {
	p := envelope.Envelope{
		ID: "env_parent", Version: "1", Issuer: envelope.Party{Agent: "support"}, Recipient: envelope.Party{Agent: "billing", Version: "1"},
		Purpose: "refund", PolicyVersion: "v1", ExpiresAt: testNow.Add(time.Hour),
		AllowedActions: []string{"orders.read", "refunds.create"}, DeniedActions: []string{"customers.delete"},
		Resources: map[string][]string{"orders": {"customer/*"}}, DataClasses: []string{"pii", "payment_metadata"},
		Approvals:  []envelope.Approval{{Condition: "refund.amount > 500", RequiredRole: "manager"}},
		Delegation: envelope.Delegation{MaxDepth: 3, CurrentDepth: 1},
	}
	raw, _ := json.Marshal(p)
	var c envelope.Envelope
	_ = json.Unmarshal(raw, &c)
	c.ID = "env_child"
	c.ParentEnvelope = p.ID
	c.Issuer = p.Recipient
	c.Recipient = envelope.Party{Agent: "notification"}
	c.Delegation.CurrentDepth = 2
	c.ExpiresAt = testNow.Add(30 * time.Minute)
	return p, c
}

func engineForTest(t testing.TB) *Engine {
	t.Helper()
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDelegationRules(t *testing.T) {
	engine := engineForTest(t)
	cases := []struct {
		name   string
		mutate func(*envelope.Envelope, *envelope.Envelope)
		code   string
	}{
		{"inherit", func(p, c *envelope.Envelope) {}, ""},
		{"narrow actions", func(p, c *envelope.Envelope) { c.AllowedActions = []string{"orders.read"} }, ""},
		{"empty grants", func(p, c *envelope.Envelope) { c.AllowedActions = nil; c.Resources = nil; c.DataClasses = nil }, ""},
		{"resource prefix narrowing", func(p, c *envelope.Envelope) { c.Resources["orders"] = []string{"customer/48319"} }, ""},
		{"resource pattern narrowing", func(p, c *envelope.Envelope) { c.Resources["orders"] = []string{"customer/subset/*"} }, ""},
		{"global category wildcard", func(p, c *envelope.Envelope) { p.Resources["orders"] = []string{"*"} }, ""},
		{"lower threshold", func(p, c *envelope.Envelope) { c.Approvals[0].Condition = "refund.amount > 300" }, ""},
		{"unconditional approval", func(p, c *envelope.Envelope) { c.Approvals[0].Condition = "true" }, ""},
		{"added requirement", func(p, c *envelope.Envelope) {
			c.Approvals = append(c.Approvals, envelope.Approval{Condition: "true", RequiredRole: "admin"})
		}, ""},
		{"added denial", func(p, c *envelope.Envelope) { c.DeniedActions = append(c.DeniedActions, "refunds.create") }, ""},
		{"new action", func(p, c *envelope.Envelope) { c.AllowedActions = append(c.AllowedActions, "orders.delete") }, "ACTION_EXPANDED"},
		{"dropped denial", func(p, c *envelope.Envelope) { c.DeniedActions = nil }, "DENIAL_REMOVED"},
		{"new data", func(p, c *envelope.Envelope) { c.DataClasses = append(c.DataClasses, "secret") }, "DATA_EXPANDED"},
		{"resource expanded", func(p, c *envelope.Envelope) { c.Resources["orders"] = []string{"*"} }, "RESOURCE_EXPANDED"},
		{"resource prefix boundary", func(p, c *envelope.Envelope) { c.Resources["orders"] = []string{"customers/1"} }, "RESOURCE_EXPANDED"},
		{"new category", func(p, c *envelope.Envelope) { c.Resources["payments"] = []string{"customer/1"} }, "RESOURCE_EXPANDED"},
		{"empty parent resources", func(p, c *envelope.Envelope) { p.Resources = nil }, "RESOURCE_EXPANDED"},
		{"long expiry", func(p, c *envelope.Envelope) { c.ExpiresAt = p.ExpiresAt.Add(time.Second) }, "EXPIRY_EXPANDED"},
		{"expired parent", func(p, c *envelope.Envelope) { p.ExpiresAt = testNow }, "INVALID_ENVELOPE"},
		{"expired child", func(p, c *envelope.Envelope) { c.ExpiresAt = testNow }, "INVALID_ENVELOPE"},
		{"changed purpose", func(p, c *envelope.Envelope) { c.Purpose = "marketing" }, "PURPOSE_CHANGED"},
		{"policy version", func(p, c *envelope.Envelope) { c.PolicyVersion = "v2" }, "POLICY_VERSION_CHANGED"},
		{"parent link", func(p, c *envelope.Envelope) { c.ParentEnvelope = "other" }, "PARENT_MISMATCH"},
		{"reused id", func(p, c *envelope.Envelope) { c.ID = p.ID }, "ID_REUSED"},
		{"issuer", func(p, c *envelope.Envelope) { c.Issuer.Agent = "imposter" }, "ISSUER_MISMATCH"},
		{"issuer version", func(p, c *envelope.Envelope) { c.Issuer.Version = "2" }, "ISSUER_MISMATCH"},
		{"max depth", func(p, c *envelope.Envelope) { c.Delegation.MaxDepth = 4 }, "DEPTH_EXPANDED"},
		{"skipped depth", func(p, c *envelope.Envelope) { c.Delegation.CurrentDepth = 3 }, "DEPTH_INVALID"},
		{"reset depth", func(p, c *envelope.Envelope) { c.Delegation.CurrentDepth = 0 }, "DEPTH_INVALID"},
		{"negative depth", func(p, c *envelope.Envelope) { c.Delegation.CurrentDepth = -1 }, "INVALID_ENVELOPE"},
		{"depth exhausted", func(p, c *envelope.Envelope) { p.Delegation.MaxDepth = 1; c.Delegation.MaxDepth = 1 }, "INVALID_ENVELOPE"},
		{"expansion flag", func(p, c *envelope.Envelope) { c.Delegation.MayExpandAuthority = true }, "INVALID_ENVELOPE"},
		{"removed approval", func(p, c *envelope.Envelope) { c.Approvals = nil }, "APPROVAL_WEAKENED"},
		{"higher threshold", func(p, c *envelope.Envelope) { c.Approvals[0].Condition = "refund.amount > 700" }, "APPROVAL_WEAKENED"},
		{"different role", func(p, c *envelope.Envelope) { c.Approvals[0].RequiredRole = "intern" }, "APPROVAL_WEAKENED"},
		{"complex unproven implication", func(p, c *envelope.Envelope) { c.Approvals[0].Condition = "refund.amount > 300 || request.vip == true" }, "APPROVAL_WEAKENED"},
		{"same complex condition", func(p, c *envelope.Envelope) {
			p.Approvals[0].Condition = "refund.amount > 500 && request.vip == true"
			c.Approvals[0] = p.Approvals[0]
		}, ""},
		{"invalid CEL", func(p, c *envelope.Envelope) { c.Approvals[0].Condition = "refund.amount >" }, "INVALID_ENVELOPE"},
		{"nonboolean CEL", func(p, c *envelope.Envelope) { c.Approvals[0].Condition = "42" }, "INVALID_ENVELOPE"},
		{"missing role", func(p, c *envelope.Envelope) { c.Approvals[0].RequiredRole = " " }, "INVALID_ENVELOPE"},
		{"missing purpose", func(p, c *envelope.Envelope) { c.Purpose = "" }, "INVALID_ENVELOPE"},
		{"version", func(p, c *envelope.Envelope) { c.Version = "2" }, "INVALID_ENVELOPE"},
		{"action glob", func(p, c *envelope.Envelope) { c.AllowedActions = []string{"orders.*"} }, "INVALID_ENVELOPE"},
		{"resource glob", func(p, c *envelope.Envelope) { c.Resources["orders"] = []string{"cust*"} }, "INVALID_ENVELOPE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c := pair()
			tc.mutate(&p, &c)
			beforeP, _ := json.Marshal(p)
			beforeC, _ := json.Marshal(c)
			result := engine.Diff(p, c, testNow)
			if tc.code == "" {
				if result.Decision != "ALLOW" {
					t.Fatalf("unexpected denial: %+v", result)
				}
			} else {
				if result.Decision != "DENY" {
					t.Fatal("expansion allowed")
				}
				found := false
				for _, v := range result.Violations {
					if v.Code == tc.code {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s: %+v", tc.code, result)
				}
			}
			afterP, _ := json.Marshal(p)
			afterC, _ := json.Marshal(c)
			if string(beforeP) != string(afterP) || string(beforeC) != string(afterC) {
				t.Fatal("inputs mutated")
			}
		})
	}
}

func TestThresholdProofBoundaries(t *testing.T) {
	e := engineForTest(t)
	for _, tc := range []struct {
		parent, child string
		allow         bool
	}{
		{"refund.amount >= 500", "refund.amount > 500", false},
		{"refund.amount > 500", "refund.amount >= 500", true},
		{"refund.amount < 500", "refund.amount < 700", true},
		{"refund.amount <= 500", "refund.amount < 500", false},
		{"refund.amount > 500", "refund.amount < 700", false},
		{"refund.amount > 500", "order.amount > 300", false},
		{"refund.amount > 500.0", "refund.amount > 300.0", false},
		{"refund.amount > 500", " refund.amount  > 500 ", true},
	} {
		p, c := pair()
		p.Approvals[0].Condition = tc.parent
		c.Approvals[0].Condition = tc.child
		got := e.Diff(p, c, testNow)
		if (got.Decision == "ALLOW") != tc.allow {
			t.Errorf("%s -> %s: %+v", tc.parent, tc.child, got)
		}
	}
}

func TestDeterministicViolations(t *testing.T) {
	e := engineForTest(t)
	p, c := pair()
	c.Resources = map[string][]string{"z": {"1", "2"}, "a": {"3"}}
	c.AllowedActions = []string{"z", "a"}
	first := e.Diff(p, c, testNow)
	for i := 0; i < 20; i++ {
		if !reflect.DeepEqual(first, e.Diff(p, c, testNow)) {
			t.Fatal("unstable output")
		}
	}
}

func TestScenarioCorpus(t *testing.T) {
	e := engineForTest(t)
	for i := 0; i < 100; i++ {
		p, c := pair()
		c.Resources["orders"] = []string{fmt.Sprintf("customer/%d", i)}
		c.Approvals[0].Condition = fmt.Sprintf("refund.amount > %d", i)
		if got := e.Diff(p, c, testNow); got.Decision != "ALLOW" {
			t.Fatalf("valid scenario %d: %+v", i, got)
		}
		c.AllowedActions = append(c.AllowedActions, fmt.Sprintf("forbidden.%d", i))
		if got := e.Diff(p, c, testNow); got.Decision != "DENY" {
			t.Fatalf("invalid scenario %d: %+v", i, got)
		}
	}
}

func FuzzDelegationCannotExpandAuthority(f *testing.F) {
	f.Add(uint64(1))
	f.Add(uint64(500))
	f.Add(^uint64(0))
	e := engineForTest(f)
	f.Fuzz(func(t *testing.T, id uint64) {
		p, c := pair()
		c.AllowedActions = append(c.AllowedActions, fmt.Sprintf("new.%d", id))
		if e.Diff(p, c, testNow).Decision != "DENY" {
			t.Fatal("expanded action accepted")
		}
	})
}

func FuzzApprovalThresholdCannotWeaken(f *testing.F) {
	f.Add(uint16(500), uint16(700))
	f.Add(uint16(300), uint16(100))
	e := engineForTest(f)
	f.Fuzz(func(t *testing.T, parent, child uint16) {
		p, c := pair()
		p.Approvals[0].Condition = fmt.Sprintf("refund.amount > %d", parent)
		c.Approvals[0].Condition = fmt.Sprintf("refund.amount > %d", child)
		if got := e.Diff(p, c, testNow); (got.Decision == "ALLOW") != (child <= parent) {
			t.Fatalf("%d -> %d: %+v", parent, child, got)
		}
	})
}

func BenchmarkDiff(b *testing.B) {
	e := engineForTest(b)
	p, c := pair()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Diff(p, c, testNow)
	}
}
