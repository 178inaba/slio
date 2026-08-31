package cmd

import (
	"fmt"
	"sort"

	"github.com/178inaba/slio/internal/config"
	"github.com/spf13/cobra"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage named workspace profiles",
	}
	cmd.AddCommand(newProfileListCmd(), newProfileUseCmd())

	return cmd
}

func newProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered profiles",
		Args:  cobra.NoArgs,
		RunE:  runProfileList,
	}
}

func newProfileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Switch the default profile",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileUse,
	}
}

func runProfileList(cmd *cobra.Command, _ []string) error {
	file, err := config.Load()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	if len(file.Profiles) == 0 {
		// Guidance for a human, not a zero-row result: a caller parsing the
		// listing should see an empty stdout.
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No profiles registered. Run `slio auth login` to add one.")
		return err
	}

	names := make([]string, 0, len(file.Profiles))
	for name := range file.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		p := file.Profiles[name]
		var err error
		if name == file.DefaultProfile {
			_, err = fmt.Fprintf(out, "%s\t%s\t(default)\n", name, p.Host)
		} else {
			_, err = fmt.Fprintf(out, "%s\t%s\n", name, p.Host)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	name := args[0]

	file, err := config.Load()
	if err != nil {
		return err
	}
	if _, ok := file.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found; registered profiles: %s", name, config.ProfileNames(file))
	}

	file.DefaultProfile = name
	if err := file.Save(); err != nil {
		return err
	}

	_, err = fmt.Fprintf(cmd.ErrOrStderr(), "Default profile set to %q.\n", name)
	return err
}
