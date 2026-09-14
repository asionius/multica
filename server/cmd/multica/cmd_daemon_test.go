package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// newDaemonFlagCmd returns a bare command carrying the daemon --port flag so
// flag-reading helpers can be exercised without touching the real commands.
func newDaemonFlagCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().Int("port", 0, "")
	return cmd
}

func TestDaemonPortFromFlag(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		if got := daemonPortFromFlag(newDaemonFlagCmd(t)); got != 0 {
			t.Fatalf("daemonPortFromFlag() = %d, want 0", got)
		}
	})
	t.Run("explicit", func(t *testing.T) {
		cmd := newDaemonFlagCmd(t)
		if err := cmd.Flags().Set("port", "12345"); err != nil {
			t.Fatal(err)
		}
		if got := daemonPortFromFlag(cmd); got != 12345 {
			t.Fatalf("daemonPortFromFlag() = %d, want 12345", got)
		}
	})
	t.Run("no port flag", func(t *testing.T) {
		if got := daemonPortFromFlag(&cobra.Command{Use: "x"}); got != 0 {
			t.Fatalf("daemonPortFromFlag() = %d, want 0", got)
		}
	})
	t.Run("nil command", func(t *testing.T) {
		if got := daemonPortFromFlag(nil); got != 0 {
			t.Fatalf("daemonPortFromFlag(nil) = %d, want 0", got)
		}
	})
}

func TestDaemonPortRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeDaemonPort("", 23456)
	if got := readDaemonPort(""); got != 23456 {
		t.Fatalf("readDaemonPort(default) = %d, want 23456", got)
	}

	// Profiles are isolated: the default profile's port must not leak.
	if got := readDaemonPort("staging"); got != 0 {
		t.Fatalf("readDaemonPort(staging) = %d, want 0 before any write", got)
	}
	writeDaemonPort("staging", 34567)
	if got := readDaemonPort("staging"); got != 34567 {
		t.Fatalf("readDaemonPort(staging) = %d, want 34567", got)
	}
	if got := readDaemonPort(""); got != 23456 {
		t.Fatalf("readDaemonPort(default) = %d, want 23456 after staging write", got)
	}
}

func TestReadDaemonPortInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := daemonPortPathForProfile("")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"", "not-a-number", "0", "-1", "70000"} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readDaemonPort(""); got != 0 {
			t.Fatalf("readDaemonPort() with %q = %d, want 0", content, got)
		}
	}
}

func TestResolveHealthPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("flag wins", func(t *testing.T) {
		writeDaemonPort("", 22222)
		if got := resolveHealthPort(11111, ""); got != 11111 {
			t.Fatalf("resolveHealthPort(11111) = %d, want 11111", got)
		}
	})

	t.Run("recorded port wins over profile default", func(t *testing.T) {
		writeDaemonPort("", 22222)
		if got := resolveHealthPort(0, ""); got != 22222 {
			t.Fatalf("resolveHealthPort(0, default) = %d, want 22222", got)
		}
	})

	t.Run("default profile fallback", func(t *testing.T) {
		os.Remove(daemonPortPathForProfile(""))
		if got := resolveHealthPort(0, ""); got != daemon.DefaultHealthPort {
			t.Fatalf("resolveHealthPort(0, default) = %d, want %d", got, daemon.DefaultHealthPort)
		}
	})

	t.Run("named profile fallback", func(t *testing.T) {
		want := healthPortForProfile("staging")
		if got := resolveHealthPort(0, "staging"); got != want {
			t.Fatalf("resolveHealthPort(0, staging) = %d, want %d", got, want)
		}
	})
}

func TestBuildDaemonStartArgsForwardsPort(t *testing.T) {
	tests := []struct {
		name       string
		healthPort int
		want       []string
	}{
		{
			name:       "port forwarded",
			healthPort: 12345,
			want:       []string{"daemon", "start", "--foreground", "--port", "12345"},
		},
		{
			name:       "no port",
			healthPort: 0,
			want:       []string{"daemon", "start", "--foreground"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDaemonStartArgs(&cobra.Command{Use: "start"}, tt.healthPort)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildDaemonStartArgs(%d) = %#v, want %#v", tt.healthPort, got, tt.want)
			}
		})
	}
}
