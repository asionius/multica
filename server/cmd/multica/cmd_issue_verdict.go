package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// issue verdict — atomic pipeline state transition.
// Replaces the old read-modify-write cycle of `issue status` + `pipeline set` calls.

var issueVerdictCmd = &cobra.Command{
	Use:   "verdict <issue-id>",
	Short: "Execute a pipeline verdict: approve, deny, or escalate",
	Long: `Issue a pipeline verdict for the given issue. The daemon executes
the state transition atomically:

  approve  → self → done, next → todo, next.bounce_with_prev += 1
  deny     → self → in_progress, prev → in_progress, prev.bounce_with_next += 1
  bounce   → self → blocked, prev → todo, prev.bounce_with_next += 1
  escalate → self.escalated = true`,
	Args: cobra.ExactArgs(1),
	RunE: runIssueVerdict,
}

func init() {
	issueCmd.AddCommand(issueVerdictCmd)
	issueVerdictCmd.Flags().String("action", "", "Verdict action: approve, deny, or escalate")
	issueVerdictCmd.Flags().String("feedback", "", "Optional human-readable feedback")
	issueVerdictCmd.MarkFlagRequired("action")
}

func runIssueVerdict(cmd *cobra.Command, args []string) error {
	action, _ := cmd.Flags().GetString("action")
	feedback, _ := cmd.Flags().GetString("feedback")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]string{"action": action}
	if feedback != "" {
		body["feedback"] = feedback
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/verdict", body, &result); err != nil {
		return fmt.Errorf("verdict: %w", err)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
