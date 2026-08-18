package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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

	// Registered before cobra would generate its own in InitDefaultHelpCmd,
	// which only builds one when none is set.
	cmd.SetHelpCommand(newHelpCmd())

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

// newHelpCmd builds the `help` command in place of the one cobra generates.
//
// Two things differ from cobra's. An unknown topic returns an error rather
// than being printed and exiting 0, which is the same failure any other bad
// argument gets. And the message reaches the user through Execute, so it
// lands on stderr whatever writers the tree carries: cobra's version prints
// it with Command.Print, which resolves to OutOrStderr — and OutOrStderr
// answers with the out writer whenever one is set, so setting one would move
// the message onto the stream reserved for data.
func newHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long: `Help provides help for any command in the application.
Simply type slio help [path to command] for full details.`,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
			target, _, err := cmd.Root().Find(args)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			if target == nil {
				target = cmd.Root()
			}

			var completions []cobra.Completion
			for _, sub := range target.Commands() {
				// IsAvailableCommand reports false for the tree's own help
				// command, and it decides that before it looks at whether
				// the command runs, so the name test is what keeps `help`
				// among its own candidates.
				if !sub.IsAvailableCommand() && sub.Name() != "help" {
					continue
				}
				if strings.HasPrefix(sub.Name(), toComplete) {
					completions = append(completions, cobra.CompletionWithDesc(sub.Name(), sub.Short))
				}
			}
			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, _, err := cmd.Root().Find(args)
			if target == nil || err != nil {
				return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
			}
			// The flag is registered lazily while executing, so a command
			// reached this way has to be given one before its help is
			// rendered, or the listing would omit --help.
			target.InitDefaultHelpFlag()
			return target.Help()
		},
	}
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

// Execute runs the root command and returns the process exit code.
//
// Nothing here catches SIGINT or SIGTERM. The Go default — terminate by the
// signal, print nothing — is what an interrupted run should do, so no error
// unwinds the stack and no failure report can be printed on that path; the
// property is structural rather than a branch that has to run first. The
// one place with work to do between the signal and the process ending arms
// its own guard around it (terminalGuard in auth.go).
func Execute() int {
	g := &globalFlags{}

	// Two statements rather than one: g.timeout is only final after flag
	// parsing, which happens inside Execute, and Go orders function calls
	// against a plain operand read only by convention, not by spec.
	err := newRootCmd(g).Execute()
	code, err := classifyFailure(err, g.timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	return code
}

// classifyFailure maps a command failure to the exit code it gets and the
// message slio prints for it, so an agent can tell "raise --timeout and
// retry" from "fix the request". The two are decided together because they
// read the same error: only an expired deadline gets a code and a rewritten
// message, and everything else keeps its own message and exits 1.
//
// The deadline comes from a context derived per command, so it is the error
// that carries it — and net/http wraps it in a *url.Error that errors.Is
// sees through.
func classifyFailure(err error, timeout time.Duration) (int, error) {
	switch {
	case err == nil:
		return 0, nil
	case errors.Is(err, context.DeadlineExceeded):
		return timeoutExitCode, fmt.Errorf(
			"timed out after %s: raise the deadline with --timeout (0 disables it): %w",
			timeout, err)
	default:
		return 1, err
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
// those prompts would fail a user who merely types slowly.
//
// A zero timeout means no deadline: context.WithTimeout(ctx, 0) would expire
// immediately, so that case returns the command's context unchanged.
func commandContext(cmd *cobra.Command, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return cmd.Context(), func() {}
	}
	return context.WithTimeout(cmd.Context(), timeout)
}
