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
// format.Format rejects a bad value while cobra parses the flags, so the
// command never runs. A command that reached the API would fail the test by
// connecting to the real Slack endpoint.
//
// pflag wraps the Set error, so the assertion is on a substring: the full
// message reads `invalid argument "yaml" for "--format" flag: invalid
// --format "yaml": must be "md" or "json"`.
func TestInvalidFormatIsRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, stderr, code := runSlio(t, "channel", "list", "--format", "yaml")
	if code == 0 {
		t.Fatal("slio channel list --format yaml: exit code = 0, want a failure for an unsupported --format")
	}
	if got := errorLine(t, stderr); !strings.Contains(got, `invalid --format "yaml"`) {
		t.Errorf("reported %q, want it to report an invalid --format", got)
	}
}

// TestFormatFlagHelpLine pins the `--format string` help line, which the
// README and skills/slio/SKILL.md document alongside the help strings. The
// type word comes from format.Format.Type, and the default from the value
// addFormatFlag seeds before registering the flag.
func TestFormatFlagHelpLine(t *testing.T) {
	root := newRootCmd(&globalFlags{})

	thread, _, err := root.Find([]string{"thread"})
	if err != nil {
		t.Fatalf("find thread command: %v", err)
	}
	flag := thread.Flags().Lookup("format")
	if flag == nil {
		t.Fatal("slio thread has no --format flag")
	}

	if got := flag.Value.Type(); got != "string" {
		t.Errorf("--format type = %q, want %q", got, "string")
	}
	if got := flag.DefValue; got != "md" {
		t.Errorf("--format default = %q, want %q", got, "md")
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

	_, stderr, code := runSlio(t, "auth", "login", "--format", "json")
	if code == 0 {
		t.Fatal("slio auth login --format json: exit code = 0, want an unknown flag failure")
	}
	if got := errorLine(t, stderr); !strings.Contains(got, "unknown flag: --format") {
		t.Errorf("reported %q, want it to report an unknown --format flag", got)
	}
}

// TestHelpCommandUnknownTopicFails covers the half of the `help` command
// slio owns rather than inherits: a topic that resolves to nothing is a
// failed invocation, not a note printed on the way to exiting 0.
func TestHelpCommandUnknownTopicFails(t *testing.T) {
	stdout, stderr, code := runSlio(t, "help", "bogus")
	if code != 1 {
		t.Fatalf("slio help bogus: exit code = %d, want 1", code)
	}
	if got := errorLine(t, stderr); !strings.Contains(got, `unknown help topic "bogus"`) {
		t.Errorf("reported %q, want it to name the topic", got)
	}
	// The help text cobra would have printed here is the reason this case
	// is worth a test: it used to land on the stream reserved for data.
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// TestHelpCommandResolvesTopics pins what the replacement kept from cobra's
// own help command, so the failure case above cannot be bought by breaking
// the ordinary ones.
func TestHelpCommandResolvesTopics(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantHelp string
	}{
		{name: "no topic is the root's own help", args: []string{"help"}, wantHelp: "slio fetches Slack threads"},
		{name: "a group", args: []string{"help", "auth"}, wantHelp: "Manage authentication"},
		{name: "a runnable command", args: []string{"help", "channel", "list"}, wantHelp: "List channels visible"},
		// Find stops at the deepest command it recognises, so the leftover
		// argument leaves the topic resolved rather than unknown.
		{name: "a trailing unknown argument keeps the topic", args: []string{"help", "auth", "bogus"}, wantHelp: "Manage authentication"},
		{name: "the help command itself", args: []string{"help", "help"}, wantHelp: "Help provides help for any command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runSlio(t, tt.args...)
			if code != 0 {
				t.Fatalf("slio %s: exit code = %d, stderr = %s", strings.Join(tt.args, " "), code, stderr)
			}
			if !strings.Contains(stdout, tt.wantHelp) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tt.wantHelp)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

// TestClassifyFailure covers the contract slio shares with cflio and rdsh:
// 0 on success, 124 when --timeout expired, 1 for everything else. An agent
// reads 124 as "raise the deadline and retry", so nothing else may produce
// it. An interrupt has no row here — it terminates the process by the
// signal, so no error reaches this function at all.
func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantCode    int
		wantContain string
	}{
		{name: "success", err: nil, wantCode: 0},
		{
			name:        "an expired deadline points at --timeout",
			err:         fmt.Errorf("fetch thread: %w", context.DeadlineExceeded),
			wantCode:    timeoutExitCode,
			wantContain: "--timeout",
		},
		{
			name:        "any other failure passes through unchanged",
			err:         errors.New("channel not found"),
			wantCode:    1,
			wantContain: "channel not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got := classifyFailure(tt.err, defaultTimeout)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if tt.err == nil {
				if got != nil {
					t.Errorf("error = %v, want nil", got)
				}
				return
			}
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
			root := newRootCmd(&globalFlags{})

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
