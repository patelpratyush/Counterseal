package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newAuditCmd() *cobra.Command {
	var baseURL string
	command := &cobra.Command{Use: "audit", Short: "Inspect persisted audit integrity"}
	verify := &cobra.Command{Use: "verify RUN_ID", Short: "Verify a run's audit chain and envelope signatures", Args: cobra.ExactArgs(1), SilenceUsage: true, RunE: func(cmd *cobra.Command, args []string) error {
		token := os.Getenv("HANDOFFGUARD_API_TOKEN")
		if token == "" {
			return fmt.Errorf("HANDOFFGUARD_API_TOKEN is required")
		}
		req, err := http.NewRequestWithContext(cmd.Context(), "POST", strings.TrimRight(baseURL, "/")+"/v1/audit/"+url.PathEscape(args[0])+"/verify", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			return fmt.Errorf("audit API returned HTTP %d", response.StatusCode)
		}
		var result struct {
			Status string `json:"status"`
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return err
		}
		var pretty bytes.Buffer
		if err = json.Indent(&pretty, raw, "", "  "); err != nil {
			return err
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout(), pretty.String()); err != nil {
			return err
		}
		if result.Status != "VALID" {
			return fmt.Errorf("audit verification failed")
		}
		return nil
	}}
	verify.Flags().StringVar(&baseURL, "url", "http://127.0.0.1:8080", "Counterseal server URL")
	command.AddCommand(verify)
	return command
}
func init() { rootCmd.AddCommand(newAuditCmd()) }
