package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditVerifyExitStatus(t *testing.T) {
	t.Setenv("HANDOFFGUARD_API_TOKEN", "test-token")
	for _, status := range []string{"VALID", "INVALID"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/audit/run_test/verify" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("incorrect audit request")
				}
				fmt.Fprintf(w, `{"status":%q}`, status)
			}))
			defer server.Close()
			cmd := newAuditCmd()
			cmd.SetArgs([]string{"verify", "run_test", "--url", server.URL})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := cmd.Execute(); (err == nil) != (status == "VALID") {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
