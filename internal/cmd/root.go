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

var (
	profileFlag        string
	formatFlag         string
	timeoutSecondsFlag int
)

var rootCmd = &cobra.Command{
	Use:   "slio",
	Short: "Read-only Slack CLI for AI coding agents",
	Long: `slio fetches Slack threads, channel history, and search results as
AI-readable Markdown (or JSON), so an AI coding agent can read Slack
discussions directly instead of relying on pasted screenshots.`,
	SilenceUsage:      true,
	PersistentPreRunE: validateFormatFlag,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "",
		"profile to use, overriding URL-based auto-selection and SLIO_PROFILE")
	rootCmd.PersistentFlags().StringVar(&formatFlag, "format", "md",
		`output format: "md" or "json"`)
	rootCmd.PersistentFlags().IntVar(&timeoutSecondsFlag, "timeout", defaultTimeoutSeconds,
		"overall deadline in seconds for the invocation (0 = no timeout)")

	rootCmd.AddCommand(threadCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(channelCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(profileCmd)
}

func validateFormatFlag(cmd *cobra.Command, args []string) error {
	if formatFlag != "md" && formatFlag != "json" {
		return fmt.Errorf(`invalid --format %q: must be "md" or "json"`, formatFlag)
	}
	return nil
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
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
func commandContext() (context.Context, context.CancelFunc) {
	if timeoutSecondsFlag <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), time.Duration(timeoutSecondsFlag)*time.Second)
}
