package policy

import (
	"handoffguard/internal/envelope"
	"reflect"
	"strings"
	"testing"
)

func TestRequiredRoles(t *testing.T) {
	c, err := NewConditions()
	if err != nil {
		t.Fatal(err)
	}
	approvals := []envelope.Approval{{Condition: "refund.amount > 500", RequiredRole: "manager"}, {Condition: "refund.amount >= 500", RequiredRole: "audit"}, {Condition: "true", RequiredRole: "audit"}}
	for _, amount := range []int64{499, 500, 501} {
		roles, err := c.RequiredRoles(approvals, map[string]any{"refund": map[string]any{"amount": amount}})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"audit"}
		if amount > 500 {
			want = append(want, "manager")
		}
		if !reflect.DeepEqual(roles, want) {
			t.Fatalf("%d: got %v want %v", amount, roles, want)
		}
	}
}

func TestConditionsFailClosed(t *testing.T) {
	c, err := NewConditions()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		condition, role string
		ctx             map[string]any
	}{
		{"refund.amount > 500", "manager", nil},
		{"refund.amount > 500", "manager", map[string]any{"refund": map[string]any{"amount": "600"}}},
		{"refund.amount >", "manager", nil},
		{"1", "manager", nil},
		{"unknown.amount > 500", "manager", nil},
		{"true", "", nil},
		{"true", " ", nil},
		{strings.Repeat(" ", 4097) + "true", "manager", nil},
	} {
		if roles, err := c.RequiredRoles([]envelope.Approval{{Condition: tc.condition, RequiredRole: tc.role}}, tc.ctx); err == nil || roles != nil {
			t.Fatalf("did not fail closed: roles=%v err=%v", roles, err)
		}
	}
}
