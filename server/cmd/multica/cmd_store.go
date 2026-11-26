package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// docStorePath converts a user key into an API path. Slashes are preserved so
// keys can be organised as "weekly/2026-11-13/report.md"; every segment is
// escaped independently so special characters cannot change the route.
func docStorePath(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	if strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("key must not start with a slash")
	}
	segments := strings.Split(key, "/")
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("key must not contain empty, dot or dot-dot segments")
		}
		escaped[i] = url.PathEscape(segment)
	}
	return "/api/doc-store/" + strings.Join(escaped, "/"), nil
}

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Personal document store (Redis-like set/get/list/del)",
	Long: `Store opaque documents (Excel, Markdown, CSV, ...) under your own keys and
read them back later. Values are durable and scoped to your account; keys are
prefix-listable.

This feature is restricted to allow-listed accounts.`,
	Example: `  # Store this week report
  multica store set weekly/2026-11-13/report.md ./report.md

  # Read it back to stdout
  multica store get weekly/2026-11-13/report.md

  # Save a copy
  multica store get weekly/2026-11-13/report.md -o /tmp/report.md

  # List this week documents
  multica store list --prefix weekly/2026-11-13/

  # Remove one document
  multica store del weekly/2026-11-13/report.md`,
}

var storeSetCmd = &cobra.Command{
	Use:   "set <key> <file|->",
	Short: "Store a document under a key (overwrites an existing value)",
	Args:  exactArgs(2),
	RunE:  runStoreSet,
}

var storeGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Read a document (stdout by default)",
	Args:  exactArgs(1),
	RunE:  runStoreGet,
}

var storeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stored document keys",
	Args:  cobra.NoArgs,
	RunE:  runStoreList,
}

var storeDeleteCmd = &cobra.Command{
	Use:     "delete <key>",
	Aliases: []string{"del", "rm"},
	Short:   "Delete a stored document",
	Args:    exactArgs(1),
	RunE:    runStoreDelete,
}

func init() {
	storeCmd.AddCommand(storeSetCmd)
	storeCmd.AddCommand(storeGetCmd)
	storeCmd.AddCommand(storeListCmd)
	storeCmd.AddCommand(storeDeleteCmd)

	storeSetCmd.Flags().String("content-type", "", "Content type (default: inferred from the file extension)")
	storeGetCmd.Flags().StringP("output", "o", "-", "Write to a file instead of stdout (use - for stdout)")
	storeListCmd.Flags().String("prefix", "", "Only list keys with this prefix")
	storeListCmd.Flags().Int("limit", 100, "Maximum number of keys to return")
	storeListCmd.Flags().Bool("json", false, "Print raw JSON instead of a table")
}

func runStoreSet(cmd *cobra.Command, args []string) error {
	key, file := args[0], args[1]
	apiPath, err := docStorePath(key)
	if err != nil {
		return err
	}

	var data []byte
	if file == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}

	contentType, _ := cmd.Flags().GetString("content-type")
	if contentType == "" && file != "-" {
		contentType = mime.TypeByExtension(filepath.Ext(file))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	// Large uploads can exceed the default 15s client timeout.
	client.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	headers, err := client.PutBytes(ctx, apiPath, data, contentType)
	if err != nil {
		return err
	}
	fmt.Printf("OK %s (%d bytes, %s, updated %s)\n", key, len(data), contentType, headers.Get("X-Doc-Updated-At"))
	return nil
}

func runStoreGet(cmd *cobra.Command, args []string) error {
	key := args[0]
	apiPath, err := docStorePath(key)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	client.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	data, _, err := client.GetBytes(ctx, apiPath)
	if err != nil {
		return err
	}

	if output == "-" || output == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", output, len(data))
	return nil
}

type docStoreListEntry struct {
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	UpdatedAt   string `json:"updated_at"`
}

type docStoreListResponse struct {
	Keys  []docStoreListEntry `json:"keys"`
	Total int64               `json:"total"`
}

func runStoreList(cmd *cobra.Command, _ []string) error {
	prefix, _ := cmd.Flags().GetString("prefix")
	limit, _ := cmd.Flags().GetInt("limit")
	asJSON, _ := cmd.Flags().GetBool("json")

	query := url.Values{}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	apiPath := "/api/doc-store"
	if encoded := query.Encode(); encoded != "" {
		apiPath += "?" + encoded
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var resp docStoreListResponse
	if err := client.GetJSON(ctx, apiPath, &resp); err != nil {
		return err
	}

	if asJSON {
		return cli.PrintJSON(os.Stdout, resp)
	}
	rows := make([][]string, len(resp.Keys))
	for i, entry := range resp.Keys {
		rows[i] = []string{entry.Key, fmt.Sprintf("%d", entry.SizeBytes), entry.UpdatedAt}
	}
	cli.PrintTable(os.Stdout, []string{"KEY", "SIZE", "UPDATED"}, rows)
	return nil
}

func runStoreDelete(cmd *cobra.Command, args []string) error {
	key := args[0]
	apiPath, err := docStorePath(key)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, apiPath); err != nil {
		return err
	}
	fmt.Printf("deleted %s\n", key)
	return nil
}
