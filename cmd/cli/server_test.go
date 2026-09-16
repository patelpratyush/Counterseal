package main

import (
	"strings"
	"testing"
)

func TestServerRequiresConfiguration(t *testing.T) {
	t.Setenv("HANDOFFGUARD_DATABASE_URL", "")
	t.Setenv("HANDOFFGUARD_API_TOKEN", "")
	cmd := newServerCmd()
	cmd.SetArgs([]string{"--key", "missing.priv"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "HANDOFFGUARD_DATABASE_URL") {
		t.Fatalf("expected configuration error, got %v", err)
	}
}
