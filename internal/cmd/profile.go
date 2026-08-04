package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage named workspace profiles",
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered profiles",
	Args:  cobra.NoArgs,
	RunE:  runProfileList,
}

var profileUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Switch the default profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileUse,
}

func init() {
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileUseCmd)
}

func runProfileList(cmd *cobra.Command, args []string) error {
	return errors.New("profile list: not implemented yet")
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	return errors.New("profile use: not implemented yet")
}
