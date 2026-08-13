package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// TestInvalidFormatIsRejected covers the wiring of the --format validation:
// it runs as the command's PreRunE, so a command must reject a bad value
// before doing any work. A command that reaches the API would fail the test
// by connecting to the real Slack endpoint.
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

// TestFormatFlagRegistration pins down which commands take --format: it is
// registered per command so the ones with no output to format reject it
// rather than silently ignoring it. `channel` is such a command — only its
// `list` subcommand emits anything.
func TestFormatFlagRegistration(t *testing.T) {
	want := map[string]bool{
		"slio thread":       true,
		"slio history":      true,
		"slio search":       true,
		"slio channel list": true,
		"slio channel":      false,
		"slio auth":         false,
		"slio auth login":   false,
		"slio profile":      false,
		"slio profile list": false,
		"slio profile use":  false,
	}

	// Walking the tree as built, rather than after a run: cobra adds its
	// own `help` and `completion` commands while executing.
	got := make(map[string]bool, len(want))
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		got[cmd.CommandPath()] = cmd.Flags().Lookup("format") != nil
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	for _, cmd := range newRootCmd(&globalFlags{}).Commands() {
		walk(cmd)
	}

	if !maps.Equal(got, want) {
		t.Errorf("commands with --format = %v, want %v", got, want)
	}
}

// TestFormatFlagIsUnknownWhereItDoesNothing is the user-visible half of
// TestFormatFlagRegistration: the flag is not merely ignored on `auth
// login`, it fails the invocation. Parsing fails before the command prompts
// for anything, so no stdin is needed.
func TestFormatFlagIsUnknownWhereItDoesNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, _, err := runSlio(t, "auth", "login", "--format", "json")
	if err == nil {
		t.Fatal("slio auth login --format json: error = nil, want an unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag: --format") {
		t.Errorf("error = %v, want it to report an unknown --format flag", err)
	}
}

// cancelledContext returns a context already past its Done, standing in for
// the signal context of a run the user interrupted.
func cancelledContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestExitCode covers the contract slio shares with cflio and rdsh: 0 on
// success, 124 when --timeout expired, 1 for everything else. An agent reads
// 124 as "raise the deadline and retry", so nothing else may produce it.
func TestExitCode(t *testing.T) {
	live := context.Background()
	interrupted := cancelledContext(t)

	tests := []struct {
		name      string
		signalCtx context.Context
		err       error
		want      int
	}{
		{name: "success", signalCtx: live, err: nil, want: 0},
		{
			name:      "an expired deadline",
			signalCtx: live,
			err:       fmt.Errorf("fetch thread: %w", context.DeadlineExceeded),
			want:      timeoutExitCode,
		},
		{
			// describeContextError reports this one as an interrupt, so the
			// code has to agree rather than reading the wrapped deadline.
			name:      "an interrupt that raced the deadline",
			signalCtx: interrupted,
			err:       fmt.Errorf("interrupted: %w", context.DeadlineExceeded),
			want:      1,
		},
		{name: "an interrupt", signalCtx: interrupted, err: errors.New("boom"), want: 1},
		{name: "any other failure", signalCtx: live, err: errors.New("boom"), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.signalCtx, tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestDescribeContextError(t *testing.T) {
	live := context.Background()
	interrupted := cancelledContext(t)

	tests := []struct {
		name        string
		signalCtx   context.Context
		err         error
		wantContain string
	}{
		{
			name:        "a deadline points at --timeout",
			signalCtx:   live,
			err:         fmt.Errorf("fetch thread: %w", context.DeadlineExceeded),
			wantContain: "--timeout",
		},
		{
			// signal.NotifyContext cancels with a cause, so the transport
			// reports that rather than context.Canceled; the signal
			// context being done is the reliable signal.
			name:        "a cancelled signal context reads as an interrupt",
			signalCtx:   interrupted,
			err:         errors.New("fetch thread: interrupt signal received"),
			wantContain: "interrupted",
		},
		{
			// The deadline is still reachable through the wrapped error, so
			// only the order of the two checks keeps this from being
			// reported as a timeout. exitCode has the matching case.
			name:        "an interrupt outranks a deadline that expired with it",
			signalCtx:   interrupted,
			err:         fmt.Errorf("fetch thread: %w", context.DeadlineExceeded),
			wantContain: "interrupted",
		},
		{
			name:        "other errors pass through unchanged",
			signalCtx:   live,
			err:         errors.New("channel not found"),
			wantContain: "channel not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeContextError(tt.signalCtx, tt.err, defaultTimeout)
			if !strings.Contains(got.Error(), tt.wantContain) {
				t.Errorf("error = %q, want it to contain %q", got, tt.wantContain)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("error = %q, want it to wrap the original error", got)
			}
		})
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
