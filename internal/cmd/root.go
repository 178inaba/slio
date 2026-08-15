package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/178inaba/slio/internal/format"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const defaultTimeout = 90 * time.Second

// timeoutExitCode follows the GNU timeout convention.
const timeoutExitCode = 124

// globalFlags carries the root persistent flag values into each command's
// RunE. newRootCmd binds them and hands the same pointer to every
// constructor that needs them, so no command or flag state lives at package
// level. The values are only final after flag parsing, so a RunE closure
// must read the fields when it runs rather than copy them at construction.
type globalFlags struct {
	profile string
	timeout time.Duration
}

func newRootCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "slio",
		Short: "Read-only Slack CLI for AI coding agents",
		Long: `slio fetches Slack threads, channel history, and search results as
AI-readable Markdown (or JSON), so an AI coding agent can read Slack
discussions directly instead of relying on pasted screenshots.`,
		// Errors are printed once in Execute; the default behavior would
		// print usage and the error again on every runtime failure, which
		// is noise for the primary (agent) consumer.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&g.profile, "profile", "",
		"profile to use, overriding URL-based auto-selection and SLIO_PROFILE")
	cmd.PersistentFlags().DurationVar(&g.timeout, "timeout", defaultTimeout,
		"overall deadline for the invocation, as a Go duration (0 = no deadline)")

	cmd.AddCommand(
		newThreadCmd(g),
		newHistoryCmd(g),
		newSearchCmd(g),
		newChannelCmd(g),
		newAuthCmd(g),
		// profile takes no globalFlags: its subcommands only read and write
		// the config file, so they resolve no workspace and issue no request.
		newProfileCmd(),
	)

	return cmd
}

// addFormatFlag registers --format on a single command. It is registered per
// command rather than on the root so it never appears on `auth` and
// `profile`, which would silently ignore it.
//
// The value carries its own validation: format.Format implements
// pflag.Value, so cobra rejects an unknown format while parsing the flags,
// before any PreRunE or RunE runs. There is no hook here for a command to
// take over, and no way to reach a command body with an unvalidated value.
func addFormatFlag(cmd *cobra.Command, outFormat *format.Format) {
	// pflag takes the flag's default from the value the variable already
	// holds, so the default lives here rather than at each declaration.
	*outFormat = format.Markdown
	cmd.Flags().Var(outFormat, "format", `output format: "md" or "json"`)
}

// interruptSignals lists the signals Execute cancels the command context
// on. It is a function so tests can assert the registered set without a
// second copy of the list to keep in step.
func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// Execute runs the root command and returns the process exit code. SIGINT
// and SIGTERM cancel the command context so in-flight requests stop instead
// of running to completion after the user has given up on them, and so
// `auth login` abandons a prompt it is waiting on.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), interruptSignals()...)
	defer stop()

	g := &globalFlags{}
	err := newRootCmd(g).ExecuteContext(ctx)
	if err != nil {
		err = describeContextError(ctx, err, g.timeout)
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	return exitCode(ctx, err)
}

// describeContextError replaces a bare context error with one that says what
// to do about it.
//
// The interrupt case is detected from the signal context rather than from
// the returned error: signal.NotifyContext cancels with a cause, which the
// transport surfaces instead of context.Canceled, so errors.Is would miss
// it. The deadline case is the other way round — it comes from a context
// derived per command, so only the error carries it, and net/http wraps it
// in a *url.Error that errors.Is sees through.
func describeContextError(signalCtx context.Context, err error, timeout time.Duration) error {
	switch {
	case signalCtx.Err() != nil:
		return fmt.Errorf("interrupted: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("timed out after %s: raise the deadline with --timeout (0 disables it): %w",
			timeout, err)
	default:
		return err
	}
}

// exitCode maps a failure to one of the three codes the sibling CLIs share,
// so an agent can tell "raise --timeout and retry" from "fix the request".
//
// The interrupt check comes first, in the same order describeContextError
// uses: describeContextError wraps the original error, so a deadline that
// expired just as a signal arrived would otherwise be reported as an
// interrupt but exit 124.
func exitCode(signalCtx context.Context, err error) int {
	switch {
	case err == nil:
		return 0
	case signalCtx.Err() != nil:
		return 1
	case errors.Is(err, context.DeadlineExceeded):
		return timeoutExitCode
	default:
		return 1
	}
}

// terminalFile reports the underlying *os.File of a command input stream
// and whether it is a real terminal. Non-*os.File readers report
// (nil, false); tests use them as scriptable stdin.
func terminalFile(in io.Reader) (*os.File, bool) {
	f, ok := in.(*os.File)
	if !ok {
		return nil, false
	}
	return f, term.IsTerminal(int(f.Fd()))
}

// commandContext returns a context bound to --timeout. It must be called at
// the point the first request is about to be issued, not at the top of RunE:
// `auth login` prompts for credentials first, and starting the clock before
// those prompts would fail a user who merely types slowly. The prompts
// still abort on Ctrl-C, because they watch cmd.Context() — the signal
// context this one derives from — directly.
//
// A zero timeout means no deadline: context.WithTimeout(ctx, 0) would expire
// immediately, so that case returns the command's context unchanged.
func commandContext(cmd *cobra.Command, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return cmd.Context(), func() {}
	}
	return context.WithTimeout(cmd.Context(), timeout)
}
