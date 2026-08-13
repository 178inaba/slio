package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const defaultTimeoutSeconds = 90

// globalFlags carries the root persistent flag values into each command's
// RunE. newRootCmd binds them and hands the same pointer to every
// constructor that needs them, so no command or flag state lives at package
// level. The values are only final after flag parsing, so a RunE closure
// must read the fields when it runs rather than copy them at construction.
type globalFlags struct {
	profile        string
	format         string
	timeoutSeconds int
}

func newRootCmd() *cobra.Command {
	var g globalFlags

	cmd := &cobra.Command{
		Use:   "slio",
		Short: "Read-only Slack CLI for AI coding agents",
		Long: `slio fetches Slack threads, channel history, and search results as
AI-readable Markdown (or JSON), so an AI coding agent can read Slack
discussions directly instead of relying on pasted screenshots.`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return validateFormat(g.format)
		},
	}

	cmd.PersistentFlags().StringVar(&g.profile, "profile", "",
		"profile to use, overriding URL-based auto-selection and SLIO_PROFILE")
	cmd.PersistentFlags().StringVar(&g.format, "format", "md",
		`output format: "md" or "json"`)
	cmd.PersistentFlags().IntVar(&g.timeoutSeconds, "timeout", defaultTimeoutSeconds,
		"overall deadline in seconds for the invocation (0 = no timeout)")

	cmd.AddCommand(
		newThreadCmd(&g),
		newHistoryCmd(&g),
		newSearchCmd(&g),
		newChannelCmd(&g),
		newAuthCmd(&g),
		newProfileCmd(),
	)

	return cmd
}

func validateFormat(format string) error {
	if format != "md" && format != "json" {
		return fmt.Errorf(`invalid --format %q: must be "md" or "json"`, format)
	}
	return nil
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
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

// commandContext returns a context bound to --timeout. A zero timeout means
// no deadline: context.WithTimeout(ctx, 0) would expire immediately, so that
// case skips WithTimeout entirely.
func commandContext(timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
}
