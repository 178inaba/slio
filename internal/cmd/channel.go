package cmd

import (
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/178inaba/slio/internal/format"
	"github.com/spf13/cobra"
)

func newChannelCmd(g *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Manage and inspect channels",
	}
	cmd.AddCommand(newChannelListCmd(g))

	return cmd
}

func newChannelListCmd(g *globalFlags) *cobra.Command {
	var outFormat format.Format

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List channels visible to the user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChannelList(cmd, g, outFormat)
		},
	}
	// `channel` itself emits nothing, so --format goes on the subcommand
	// that does. Local flags are not inherited, so registering it on the
	// group would leave `channel list --format` unknown.
	addFormatFlag(cmd, &outFormat)

	return cmd
}

func runChannelList(cmd *cobra.Command, g *globalFlags, outFormat format.Format) error {
	ctx, cancel := commandContext(cmd, g.timeout)
	defer cancel()

	creds, _, cacheKey, err := resolveWorkspace(ctx, g.profile, "")
	if err != nil {
		return err
	}
	client := slackClientFactory(creds.Token)

	store, err := cache.Open(cacheKey)
	if err != nil {
		return err
	}

	channels, err := client.ConversationsForUser(ctx)
	if err != nil {
		return err
	}
	if err := cacheChannels(store, channels, time.Now()); err != nil {
		return err
	}

	list := make([]format.Channel, len(channels))
	for i, c := range channels {
		list[i] = format.Channel{ID: c.ID, Name: c.Name}
	}
	return format.WriteChannels(cmd.OutOrStdout(), outFormat, list)
}
