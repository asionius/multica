package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestBuildCodeBuddyArgsIncludesProtocolFlags(t *testing.T) {
	t.Parallel()

	args := buildCodeBuddyArgs(ExecOptions{}, slog.Default())
	expected := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
		"--permission-mode", "bypassPermissions",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestBuildCodeBuddyArgsWithOptions(t *testing.T) {
	t.Parallel()

	args := buildCodeBuddyArgs(ExecOptions{
		Model:           "claude-sonnet-4",
		MaxTurns:        25,
		SystemPrompt:    "be terse",
		ResumeSessionID: "sess-abc",
	}, slog.Default())

	joined := " " + stringsJoin(args, " ") + " "
	for _, want := range []string{
		" --model claude-sonnet-4 ",
		" --max-turns 25 ",
		" --append-system-prompt be terse ",
		" --resume sess-abc ",
	} {
		if !containsSubstring(joined, want) {
			t.Fatalf("missing %q in args: %v", want, args)
		}
	}
}

func TestBuildCodeBuddyArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildCodeBuddyArgs(ExecOptions{
		CustomArgs: []string{
			"--output-format", "text",
			"--permission-mode=plan",
			"-p",
			"--model", "custom-model",
		},
	}, slog.Default())

	// --model custom-model should pass through.
	foundModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "custom-model" {
			foundModel = true
		}
		// No "--output-format text" leak.
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "text" {
			t.Fatalf("blocked --output-format text should have been filtered: %v", args)
		}
	}
	if !foundModel {
		t.Fatalf("expected --model custom-model to pass through: %v", args)
	}

	// The leading hardcoded `-p` should be present exactly once.
	pCount := 0
	for _, a := range args {
		if a == "-p" {
			pCount++
		}
	}
	if pCount != 1 {
		t.Fatalf("expected exactly one -p flag, got %d: %v", pCount, args)
	}
}

func TestWriteCodeBuddyInputShape(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := writeCodeBuddyInput(&buf, "ping"); err != nil {
		t.Fatalf("writeCodeBuddyInput: %v", err)
	}
	data := buf.Bytes()
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected newline-terminated payload, got %q", data)
	}

	var payload map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["type"] != "user" {
		t.Fatalf("expected type user, got %v", payload["type"])
	}
	msg, ok := payload["message"].(map[string]any)
	if !ok {
		t.Fatalf("expected message object, got %T", payload["message"])
	}
	if msg["role"] != "user" {
		t.Fatalf("expected role user, got %v", msg["role"])
	}
	content, ok := msg["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("expected one content block, got %v", msg["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "ping" {
		t.Fatalf("unexpected block: %v", block)
	}
}

// ── Test helpers ──

func stringsJoin(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}

func containsSubstring(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}
