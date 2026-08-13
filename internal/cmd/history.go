package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/178inaba/slio/internal/format"
	"github.com/178inaba/slio/internal/parse"
	"github.com/178inaba/slio/internal/slackclient"
	"github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

const defaultHistoryLimit = 50

func newHistoryCmd(g *globalFlags) *cobra.Command {
	var (
		limit     int
		since     string
		until     string
		outFormat string
	)

	cmd := &cobra.Command{
		Use:   "history <channel>",
		Short: "Fetch recent channel history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHistory(cmd, args, g, limit, since, until, outFormat)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", defaultHistoryLimit,
		"maximum number of messages to fetch")
	cmd.Flags().StringVar(&since, "since", "",
		"only messages after this time (ISO 8601 or relative, e.g. 24h)")
	cmd.Flags().StringVar(&until, "until", "",
		"only messages before this time (ISO 8601 or relative, e.g. 24h)")
	addFormatFlag(cmd, &outFormat)

	return cmd
}

func runHistory(cmd *cobra.Command, args []string, g *globalFlags, limit int, since, until, outFormat string) error {
	chArg, err := parse.ParseChannelArg(args[0])
	if err != nil {
		return err
	}

	ctx, cancel := commandContext(cmd, g.timeout)
	defer cancel()

	creds, host, cacheKey, err := resolveWorkspace(ctx, g.profile, chArg.Host)
	if err != nil {
		return err
	}
	client := slackClientFactory(creds.Token)

	store, err := cache.Open(cacheKey)
	if err != nil {
		return err
	}

	now := time.Now()

	channelID := chArg.ID
	if chArg.Name != "" {
		channelID, err = resolveChannelID(ctx, client, store, chArg.Name, now)
		if err != nil {
			return err
		}
	}

	rangeSet := since != "" || until != ""
	oldestTs, err := parseTimeFlag("--since", since, now)
	if err != nil {
		return err
	}
	latestTs, err := parseTimeFlag("--until", until, now)
	if err != nil {
		return err
	}

	msgs, hasMore, err := client.ConversationHistory(ctx, channelID, oldestTs, latestTs, limit)
	if err != nil {
		return err
	}
	reverseMessages(msgs)

	resolver := newUserResolver(ctx, client, store, now)
	messages := make([]format.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Channel == "" {
			m.Channel = channelID
		}
		fm, err := messageFromMsg(m, host, resolver.resolve, true)
		if err != nil {
			return err
		}
		messages = append(messages, fm)
	}
	if err := resolver.err(); err != nil {
		return err
	}

	var notice string
	if hasMore {
		if rangeSet {
			notice = "older messages remain within the range — raise --limit"
		} else {
			notice = "older messages omitted — raise --limit or narrow with --since"
		}
	}

	return writeMessages(cmd, outFormat, messages, resolver.resolve, notice, "")
}

// parseTimeFlag parses a --since/--until value into a Slack ts bound, or
// returns "" unset when raw is empty. name (e.g. "--since") labels the
// error on a parse failure.
func parseTimeFlag(name, raw string, now time.Time) (string, error) {
	if raw == "" {
		return "", nil
	}
	t, err := parse.ParseTime(raw, now)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return format.FormatTs(t), nil
}

// resolveChannelID resolves a "#name" channel argument to a channel ID via
// the cache, refreshing from users.conversations on a miss (name not
// cached, or the cached entry is past its TTL).
func resolveChannelID(ctx context.Context, client *slackclient.Client, store *cache.Store, name string, now time.Time) (string, error) {
	if id, ok, err := store.ChannelIDByName(name, now); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}

	channels, err := client.ConversationsForUser(ctx)
	if err != nil {
		return "", err
	}
	if err := cacheChannels(store, channels, now); err != nil {
		return "", err
	}

	for _, c := range channels {
		if c.Name == name {
			return c.ID, nil
		}
	}
	return "", fmt.Errorf("channel #%s not found among the channels you're a member of", name)
}

func cacheChannels(store *cache.Store, channels []slack.Channel, now time.Time) error {
	infos := make([]cache.ChannelInfo, len(channels))
	for i, c := range channels {
		infos[i] = cache.ChannelInfo{ID: c.ID, Name: c.Name}
	}
	return store.PutChannels(infos, now)
}

// reverseMessages reverses a message slice in place. conversations.history
// returns newest-first; history output is oldest-first so an agent reads
// the conversation top-down.
func reverseMessages(msgs []slack.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
