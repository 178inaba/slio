package cmd

import (
	"fmt"
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/178inaba/slio/internal/format"
	"github.com/spf13/cobra"
)

const defaultSearchLimit = 20

func newSearchCmd(g *globalFlags) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search messages (Slack search syntax pass-through)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, args, g, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", defaultSearchLimit,
		"maximum number of results to fetch")

	return cmd
}

func runSearch(cmd *cobra.Command, args []string, g *globalFlags, limit int) error {
	query := args[0]

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

	matches, total, err := client.SearchMessages(ctx, query, limit)
	if err != nil {
		return err
	}

	resolver := newUserResolver(ctx, client, store, time.Now())
	messages := make([]format.Message, 0, len(matches))
	for _, m := range matches {
		fm, err := messageFromSearchMatch(m, resolver.resolve)
		if err != nil {
			return err
		}
		messages = append(messages, fm)
	}
	if err := resolver.err(); err != nil {
		return err
	}

	var notice string
	if more := total - len(messages); more > 0 {
		notice = fmt.Sprintf("%d more results", more)
	}

	return writeMessages(cmd, g.format, messages, resolver.resolve, "", notice)
}
