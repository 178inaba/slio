package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestInvalidFormatIsRejected covers the wiring of the --format validation:
// it runs as the root's PersistentPreRunE, so a subcommand must reject a bad
// value before doing any work. A command that reaches the API would fail the
// test by connecting to the real Slack endpoint.
func TestInvalidFormatIsRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, _, err := runSlio(t, "channel", "list", "--format", "yaml")
	if err == nil {
		t.Fatal("slio channel list --format yaml: error = nil, want error for an unsupported --format")
	}
	if !strings.Contains(err.Error(), "invalid --format") {
		t.Errorf("error = %v, want it to report an invalid --format", err)
	}
}

func TestCommandContextTimeout(t *testing.T) {
	tests := []struct {
		name         string
		timeout      time.Duration
		wantDeadline bool
	}{
		{name: "zero means no deadline", timeout: 0, wantDeadline: false},
		{name: "negative means no deadline", timeout: -time.Second, wantDeadline: false},
		{name: "positive sets a deadline", timeout: time.Minute, wantDeadline: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			// Command.Context returns the stored context as-is, and cobra
			// only fills it in while executing; a command built by hand
			// would hand commandContext a nil parent.
			cmd.SetContext(context.Background())

			ctx, cancel := commandContext(cmd, tt.timeout)
			defer cancel()

			if _, ok := ctx.Deadline(); ok != tt.wantDeadline {
				t.Errorf("ctx.Deadline() ok = %v, want %v", ok, tt.wantDeadline)
			}
		})
	}
}

// TestTimeoutFlagTakesADuration pins the flag to the Go duration form shared
// with the sibling CLIs: a bare number is rejected at parse time rather than
// being read as seconds.
//
// It stops at ParseFlags instead of running a command: a value that parses
// would go on to issue a real request, since resolveWorkspace picks up a
// SLIO_TOKEN left in the environment.
func TestTimeoutFlagTakesADuration(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "a bare number is missing its unit", value: "90", wantErr: true},
		{name: "a Go duration parses", value: "90s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, _, _ := newTestRoot(t)

			err := root.ParseFlags([]string{"--timeout", tt.value})
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("--timeout %s: %v", tt.value, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("--timeout %s: error = nil, want a parse error", tt.value)
			}
			if !strings.Contains(err.Error(), "missing unit in duration") {
				t.Errorf("error = %v, want it to report the missing duration unit", err)
			}
		})
	}
}
