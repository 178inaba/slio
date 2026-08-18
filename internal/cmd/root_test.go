package cmd

import (
	"bytes"
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
	root := newRootCmd(&globalFlags{}, &unknownCommand{})

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
	for _, cmd := range newRootCmd(&globalFlags{}, &unknownCommand{}).Commands() {
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

// groupCommands are the commands that carry subcommands and nothing else.
// `completion` is cobra's, generated while executing rather than built by
// newRootCmd, which is why the fix has to reach the whole tree instead of
// setting a field on each of slio's own constructors.
var groupCommands = []string{"auth", "channel", "profile", "completion"}

// TestUnknownSubcommandUnderGroupIsReported covers the case cobra answers
// with a help request rather than an error: the command has no Run, so
// Command.execute returns flag.ErrHelp before it ever validates the
// arguments. Left alone that prints help to stdout and exits 0, which an
// agent branching on the exit code cannot tell from success.
func TestUnknownSubcommandUnderGroupIsReported(t *testing.T) {
	for _, group := range groupCommands {
		t.Run(group, func(t *testing.T) {
			stdout, stderr, code := runSlio(t, group, "bogus")
			if code != 1 {
				t.Errorf("slio %s bogus: exit code = %d, want 1", group, code)
			}
			want := fmt.Sprintf(`Error: unknown command "bogus" for "slio %s"`, group)
			if got := errorLine(t, stderr); got != want {
				t.Errorf("reported %q, want %q", got, want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

// TestUnknownSubcommandSuggestsNearMatch covers the half of the message a
// test built on `bogus` alone never reaches: nothing is close enough to
// `bogus` to be suggested, so only a real typo exercises the candidates.
func TestUnknownSubcommandSuggestsNearMatch(t *testing.T) {
	_, stderr, code := runSlio(t, "channel", "lsit")
	if code != 1 {
		t.Errorf("slio channel lsit: exit code = %d, want 1", code)
	}
	want := `Error: unknown command "lsit" for "slio channel"`
	if got := errorLine(t, stderr); got != want {
		t.Errorf("reported %q, want %q", got, want)
	}
	if !strings.Contains(stderr, "Did you mean this?") || !strings.Contains(stderr, "list") {
		t.Errorf("stderr = %q, want it to suggest `list`", stderr)
	}
}

// TestGroupHelpArgumentSuggestsTheFlag covers `help` as an argument to a
// group. cobra only ever suggests registered subcommands, and `help` is
// registered on the root alone, so the flag has to be offered explicitly.
func TestGroupHelpArgumentSuggestsTheFlag(t *testing.T) {
	for _, group := range groupCommands {
		t.Run(group, func(t *testing.T) {
			stdout, stderr, code := runSlio(t, group, "help")
			if code != 1 {
				t.Errorf("slio %s help: exit code = %d, want 1", group, code)
			}
			want := fmt.Sprintf(`Error: unknown command "help" for "slio %s"`, group)
			if got := errorLine(t, stderr); got != want {
				t.Errorf("reported %q, want %q", got, want)
			}
			if !strings.Contains(stderr, "--help") {
				t.Errorf("stderr = %q, want it to suggest --help", stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

// TestHelpRequestsAreUnaffected is the other side of the override: every way
// of actually asking for help still prints it to stdout and succeeds. The
// `help` command rows matter most — its RunE calls Help(), which resolves to
// the override, and only the empty argument list carries them past it.
func TestHelpRequestsAreUnaffected(t *testing.T) {
	argLists := [][]string{
		{}, {"--help"},
		{"help"}, {"help", "auth"},
	}
	for _, group := range groupCommands {
		argLists = append(argLists, []string{group}, []string{group, "--help"})
	}

	for _, args := range argLists {
		t.Run("slio "+strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := runSlio(t, args...)
			if code != 0 {
				t.Errorf("exit code = %d, want 0; stderr = %s", code, stderr)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("stdout = %q, want the help text", stdout)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

// TestUnknownCommandAtRootIsUnchanged guards the case cobra already reports
// itself, through legacyArgs. The override must not take it over, or the
// message would change shape.
func TestUnknownCommandAtRootIsUnchanged(t *testing.T) {
	stdout, stderr, code := runSlio(t, "bogus")
	if code != 1 {
		t.Errorf("slio bogus: exit code = %d, want 1", code)
	}
	want := `Error: unknown command "bogus" for "slio"`
	if got := errorLine(t, stderr); got != want {
		t.Errorf("reported %q, want %q", got, want)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

// TestRunnableCommandsStillValidateArgs guards the commands that never took
// this path: they have a Run, so Command.execute reaches ValidateArgs and
// reports a bad argument list itself. The override must leave them there.
func TestRunnableCommandsStillValidateArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantContain string
	}{
		{
			name:        "too many arguments",
			args:        []string{"channel", "list", "bogus"},
			wantContain: `unknown command "bogus" for "slio channel list"`,
		},
		{
			name:        "too few arguments",
			args:        []string{"profile", "use"},
			wantContain: "accepts 1 arg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			_, stderr, code := runSlio(t, tt.args...)
			if code != 1 {
				t.Errorf("slio %s: exit code = %d, want 1", strings.Join(tt.args, " "), code)
			}
			if got := errorLine(t, stderr); !strings.Contains(got, tt.wantContain) {
				t.Errorf("reported %q, want it to contain %q", got, tt.wantContain)
			}
		})
	}
}

// TestGroupTypoIsRecordedNotReturned pins the mechanism the rest of these
// tests only see the result of: cobra returns nil for a mistyped subcommand
// under a group, so the exit code comes from the recorded failure rather
// than from an error travelling up. If a future cobra started returning one
// here, everything above would still pass while the message came from a
// different place entirely.
func TestGroupTypoIsRecordedNotReturned(t *testing.T) {
	u := &unknownCommand{}
	root := newRootCmd(&globalFlags{}, u)
	// SetArgs is not optional: a root with no argument list falls back to
	// os.Args[1:], which under `go test` is the test binary's own flags.
	root.SetArgs([]string{"auth", "bogus"})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	if err := root.Execute(); err != nil {
		t.Errorf("root.Execute() error = %v, want nil — cobra answers this case as a help request", err)
	}
	if !u.reported {
		t.Error("unknownCommand.reported = false, want the failure recorded")
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
		// wantNoMessage marks the rows that carry an error but must produce
		// no message, so nothing is printed twice.
		wantNoMessage bool
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
		{
			name:          "an unknown command was already reported by the help function",
			err:           errUnknownCommand,
			wantCode:      1,
			wantNoMessage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got := classifyFailure(tt.err, defaultTimeout)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if tt.err == nil || tt.wantNoMessage {
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
			root := newRootCmd(&globalFlags{}, &unknownCommand{})

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
