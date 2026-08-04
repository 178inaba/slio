package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var threadDownloadFlag bool

var threadCmd = &cobra.Command{
	Use:   "thread <url>",
	Short: "Fetch a full thread by its Slack message permalink",
	Args:  cobra.ExactArgs(1),
	RunE:  runThread,
}

func init() {
	threadCmd.Flags().BoolVar(&threadDownloadFlag, "download", false,
		"download attachments to a local temp directory and print their paths")
}

func runThread(cmd *cobra.Command, args []string) error {
	return errors.New("thread: not implemented yet")
}
