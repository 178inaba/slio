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

const defaultTimeout = 90 * time.Second

// globalFlags carries the root persistent flag values into each command's
// RunE. newRootCmd binds them and hands the same pointer to every
// constructor that needs them, so no command or flag state lives at package
// level. The values are only final after flag parsing, so a RunE closure
// must read the fields when it runs rather than copy them at construction.
type globalFlags struct {
	profile string
	format  string
	timeout time.Duration
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
	cmd.PersistentFlags().DurationVar(&g.timeout, "timeout", defaultTimeout,
		"overall deadline for the invocation, as a Go duration (0 = no deadline)")

	cmd.AddCommand(
		newThreadCmd(&g),
		newHistoryCmd(&g),
		newSearchCmd(&g),
		newChannelCmd(&g),
		newAuthCmd(&g),
		// profile takes no globalFlags: its subcommands only read and write
		// the config file, so they resolve no workspace and issue no request.
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
