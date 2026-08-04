package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

const defaultHistoryLimit = 50

var (
	historyLimitFlag int
	historySinceFlag string
	historyUntilFlag string
)

var historyCmd = &cobra.Command{
	Use:   "history <channel>",
	Short: "Fetch recent channel history",
	Args:  cobra.ExactArgs(1),
	RunE:  runHistory,
}

func init() {
	historyCmd.Flags().IntVar(&historyLimitFlag, "limit", defaultHistoryLimit,
		"maximum number of messages to fetch")
	historyCmd.Flags().StringVar(&historySinceFlag, "since", "",
		"only messages after this time (ISO 8601 or relative, e.g. 24h)")
	historyCmd.Flags().StringVar(&historyUntilFlag, "until", "",
		"only messages before this time (ISO 8601 or relative, e.g. 24h)")
}

func runHistory(cmd *cobra.Command, args []string) error {
	return errors.New("history: not implemented yet")
}
