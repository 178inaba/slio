package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
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
