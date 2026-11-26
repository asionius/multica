package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newStoreTestCmd(sub string) *cobra.Command {
	cmd := &cobra.Command{Use: sub}
	switch sub {
	case "set":
		cmd.Flags().String("content-type", "", "")
	case "get":
		cmd.Flags().StringP("output", "o", "-", "")
	case "list":
		cmd.Flags().String("prefix", "", "")
		cmd.Flags().Int("limit", 100, "")
		cmd.Flags().Bool("json", false, "")
	}
	return cmd
}

func TestDocStorePath(t *testing.T) {
	cases := []struct {
		key  string
		ok   bool
		path string
	}{
		{"weekly/2026-11-13/report.md", true, "/api/doc-store/weekly/2026-11-13/report.md"},
		{"a b/c.md", true, "/api/doc-store/a%20b/c.md"},
		{"", false, ""},
		{"/abs", false, ""},
		{"a//b", false, ""},
		{"a/../b", false, ""},
	}
	for _, tc := range cases {
		got, err := docStorePath(tc.key)
		if tc.ok {
			if err != nil {
				t.Fatalf("docStorePath(%q): %v", tc.key, err)
			}
			if got != tc.path {
				t.Fatalf("docStorePath(%q) = %q, want %q", tc.key, got, tc.path)
			}
		} else if err == nil {
			t.Fatalf("docStorePath(%q): expected error", tc.key)
		}
	}
}

func TestRunStoreSetUploadsRawBytes(t *testing.T) {
	var gotBody []byte
	var gotType, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("X-Doc-Updated-At", "2026-11-13T12:00:00Z")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	dir := t.TempDir()
	file := filepath.Join(dir, "report.md")
	want := []byte("hello weekly")
	if err := os.WriteFile(file, want, 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	cmd := newStoreTestCmd("set")
	if err := runStoreSet(cmd, []string{"weekly/2026-11-13/report.md", file}); err != nil {
		t.Fatalf("runStoreSet: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotPath != "/api/doc-store/weekly/2026-11-13/report.md" {
		t.Fatalf("path = %s", gotPath)
	}
	if string(gotBody) != string(want) {
		t.Fatalf("body = %q", gotBody)
	}
	if !strings.HasPrefix(gotType, "text/markdown") {
		t.Fatalf("content type = %q", gotType)
	}
}

func TestRunStoreGetWritesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/doc-store/a/b" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	out := filepath.Join(t.TempDir(), "out.txt")
	cmd := newStoreTestCmd("get")
	if err := cmd.Flags().Set("output", out); err != nil {
		t.Fatalf("set output: %v", err)
	}
	if err := runStoreGet(cmd, []string{"a/b"}); err != nil {
		t.Fatalf("runStoreGet: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("output = %q", data)
	}
}

func TestRunStoreListPassesPrefix(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/doc-store" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[{"key":"weekly/a.md","size_bytes":3,"updated_at":"2026-11-13T12:00:00Z"}],"total":1}`))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newStoreTestCmd("list")
	if err := cmd.Flags().Set("prefix", "weekly/"); err != nil {
		t.Fatalf("set prefix: %v", err)
	}
	if err := runStoreList(cmd, nil); err != nil {
		t.Fatalf("runStoreList: %v", err)
	}
	if !strings.Contains(gotQuery, "prefix=weekly%2F") {
		t.Fatalf("query = %q", gotQuery)
	}
}

func TestRunStoreDelete(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := newStoreTestCmd("delete")
	if err := runStoreDelete(cmd, []string{"weekly/x.md"}); err != nil {
		t.Fatalf("runStoreDelete: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/doc-store/weekly/x.md" {
		t.Fatalf("got %s %s", gotMethod, gotPath)
	}
}
