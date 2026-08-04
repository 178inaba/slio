package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

const defaultSearchLimit = 20

var searchLimitFlag int

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search messages (Slack search syntax pass-through)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	searchCmd.Flags().IntVar(&searchLimitFlag, "limit", defaultSearchLimit,
		"maximum number of results to fetch")
}

func runSearch(cmd *cobra.Command, args []string) error {
	return errors.New("search: not implemented yet")
}
