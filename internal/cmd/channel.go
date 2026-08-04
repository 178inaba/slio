package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/178inaba/slio/internal/cache"
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

type jsonChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func runChannelList(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	creds, _, cacheKey, err := resolveWorkspace(ctx, "")
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
	if formatFlag == "json" {
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
	}

	for _, c := range channels {
		if _, err := fmt.Fprintf(out, "#%s\t%s\n", c.Name, c.ID); err != nil {
			return err
		}
	}
	return nil
}
