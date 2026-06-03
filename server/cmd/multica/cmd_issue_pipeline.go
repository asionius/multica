package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// issue pipeline get/set — read/write the structured pipeline_state columns
// added by migration 097. By convention the pipeline-initializer agent owns
// the "set" path; stage agents read but do not write directly.

var issuePipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Read or write an issue's structured pipeline state (pipeline_kind / pipeline_base / pipeline_dynamic)",
	Long: "The pipeline state was extracted from the issue.description YAML block by " +
		"migration 097. pipeline_base holds static topology (siblings map, version, " +
		"branch, bounce_cap) so the prefix prompt cache stays warm across ping-pongs; " +
		"pipeline_dynamic holds mutable counters that should be passed via wakeup " +
		"payload, not embedded in the cached prefix.",
}

var issuePipelineGetCmd = &cobra.Command{
	Use:   "get <issue-id>",
	Short: "Print an issue's pipeline state as JSON",
	Args:  exactArgs(1),
	RunE:  runIssuePipelineGet,
}

var issuePipelineSetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Update an issue's pipeline_base or pipeline_dynamic",
	Long: "Pass exactly one of:\n" +
		"  --base <json> | --base-stdin | --base-file <path>      (also requires --kind)\n" +
		"  --dynamic <json> | --dynamic-stdin | --dynamic-file <path>\n\n" +
		"--kind sets pipeline_kind (e.g. ccproc-top, ccproc-stage, ccproc-packager) " +
		"and is required when writing --base. Dynamic updates do not change kind.",
	Args: exactArgs(1),
	RunE: runIssuePipelineSet,
}

func init() {
	issueCmd.AddCommand(issuePipelineCmd)
	issuePipelineCmd.AddCommand(issuePipelineGetCmd)
	issuePipelineCmd.AddCommand(issuePipelineSetCmd)

	issuePipelineGetCmd.Flags().String("output", "json", "Output format: json (only)")
	issuePipelineGetCmd.Flags().String("field", "", "Print only one field: kind | base | dynamic (default: full object)")

	issuePipelineSetCmd.Flags().String("kind", "", "Pipeline kind label, required when writing --base")
	issuePipelineSetCmd.Flags().String("base", "", "Inline JSON for pipeline_base")
	issuePipelineSetCmd.Flags().Bool("base-stdin", false, "Read pipeline_base JSON from stdin")
	issuePipelineSetCmd.Flags().String("base-file", "", "Read pipeline_base JSON from a UTF-8 file")
	issuePipelineSetCmd.Flags().String("dynamic", "", "Inline JSON for pipeline_dynamic")
	issuePipelineSetCmd.Flags().Bool("dynamic-stdin", false, "Read pipeline_dynamic JSON from stdin")
	issuePipelineSetCmd.Flags().String("dynamic-file", "", "Read pipeline_dynamic JSON from a UTF-8 file")
	issuePipelineSetCmd.Flags().String("output", "json", "Output format: json (only)")
}

func runIssuePipelineGet(cmd *cobra.Command, args []string) error {
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

	var resp map[string]json.RawMessage
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/pipeline", &resp); err != nil {
		return fmt.Errorf("get pipeline: %w", err)
	}

	field, _ := cmd.Flags().GetString("field")
	if field != "" {
		val, ok := resp[field]
		if !ok {
			return fmt.Errorf("unknown --field %q (expected: kind, base, dynamic)", field)
		}
		_, err := os.Stdout.Write(append([]byte(val), '\n'))
		return err
	}
	return cli.PrintJSON(os.Stdout, resp)
}

func runIssuePipelineSet(cmd *cobra.Command, args []string) error {
	base, hasBase, err := resolveTextFlag(cmd, "base")
	if err != nil {
		return err
	}
	dyn, hasDyn, err := resolveTextFlag(cmd, "dynamic")
	if err != nil {
		return err
	}
	if hasBase == hasDyn {
		return fmt.Errorf("specify exactly one of --base / --dynamic (with their -stdin/-file variants)")
	}

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

	var path string
	body := map[string]any{}

	if hasBase {
		kind, _ := cmd.Flags().GetString("kind")
		if kind == "" {
			return fmt.Errorf("--kind is required when writing --base")
		}
		var baseObj json.RawMessage
		if err := json.Unmarshal([]byte(base), &baseObj); err != nil {
			return fmt.Errorf("--base must be valid JSON: %w", err)
		}
		body["kind"] = kind
		body["base"] = baseObj
		path = "/api/issues/" + issueRef.ID + "/pipeline/base"
	} else {
		var dynObj json.RawMessage
		if err := json.Unmarshal([]byte(dyn), &dynObj); err != nil {
			return fmt.Errorf("--dynamic must be valid JSON: %w", err)
		}
		body["dynamic"] = dynObj
		path = "/api/issues/" + issueRef.ID + "/pipeline/dynamic"
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update pipeline: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}
