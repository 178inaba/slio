package cmd

import (
	"encoding/json"
	"fmt"
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

type jsonChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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

	out := cmd.OutOrStdout()
	switch outFormat {
	case format.JSON:
		list := make([]jsonChannel, len(channels))
		for i, c := range channels {
			list[i] = jsonChannel{ID: c.ID, Name: c.Name}
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(data))
		return err
	case format.Markdown:
		for _, c := range channels {
			if _, err := fmt.Fprintf(out, "#%s\t%s\n", c.Name, c.ID); err != nil {
				return err
			}
		}
		return nil
	default:
		return format.UnsupportedError(outFormat)
	}
}
