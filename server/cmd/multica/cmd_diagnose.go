package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Upload session diagnostics for maintainer review",
}

var diagnoseUploadCmd = &cobra.Command{
	Use:     "upload <archive.tar.gz>",
	Short:   "Upload a packed session archive",
	Long:    "Upload a tar.gz of the current session (index.json + messages/) so maintainers can inspect it.",
	Example: `  $ multica diagnose upload /tmp/session.tar.gz --session-id abc --output json`,
	Args:    exactArgs(1),
	RunE:    runDiagnoseUpload,
}

func init() {
	diagnoseCmd.AddCommand(diagnoseUploadCmd)
	diagnoseUploadCmd.Flags().String("session-id", "", "Session ID the archive belongs to")
	diagnoseUploadCmd.Flags().String("hostname", "", "Hostname of the machine that packed the session")
	diagnoseUploadCmd.Flags().String("plugin-version", "", "Plugin version that created the archive")
	diagnoseUploadCmd.Flags().String("note", "", "Optional note for maintainers")
	diagnoseUploadCmd.Flags().String("output", "json", "Output format: json")
	_ = diagnoseUploadCmd.MarkFlagRequired("session-id")
}

func runDiagnoseUpload(cmd *cobra.Command, args []string) error {
	sessionID, _ := cmd.Flags().GetString("session-id")
	if sessionID == "" {
		return fmt.Errorf("--session-id is required")
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("archive is empty")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	hostname, _ := cmd.Flags().GetString("hostname")
	pluginVersion, _ := cmd.Flags().GetString("plugin-version")
	note, _ := cmd.Flags().GetString("note")

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	result, err := client.UploadDiagnostic(ctx, data, args[0], cli.DiagnosticUploadOptions{
		SessionID:     sessionID,
		Hostname:      hostname,
		PluginVersion: pluginVersion,
		Note:          note,
	})
	if err != nil {
		return fmt.Errorf("upload diagnostic: %w", err)
	}

	// --output is registered so the plugin's `--output json` is not an
	// unknown flag. The upload contract is always JSON on stdout.
	_, _ = cmd.Flags().GetString("output")
	return cli.PrintJSON(os.Stdout, result)
}
