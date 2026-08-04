package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Manage and inspect channels",
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels visible to the user",
	Args:  cobra.NoArgs,
	RunE:  runChannelList,
}

func init() {
	channelCmd.AddCommand(channelListCmd)
}

func runChannelList(cmd *cobra.Command, args []string) error {
	return errors.New("channel list: not implemented yet")
}
