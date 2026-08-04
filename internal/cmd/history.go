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
	chArg, err := parse.ParseChannelArg(args[0])
	if err != nil {
		return err
	}

	ctx, cancel := commandContext()
	defer cancel()

	creds, host, cacheKey, err := resolveWorkspace(ctx, chArg.Host)
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

	var oldestTs, latestTs string
	rangeSet := historySinceFlag != "" || historyUntilFlag != ""
	if historySinceFlag != "" {
		t, err := parse.ParseTime(historySinceFlag, now)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
		oldestTs = format.FormatTs(t)
	}
	if historyUntilFlag != "" {
		t, err := parse.ParseTime(historyUntilFlag, now)
		if err != nil {
			return fmt.Errorf("--until: %w", err)
		}
		latestTs = format.FormatTs(t)
	}

	msgs, hasMore, err := client.ConversationHistory(ctx, channelID, oldestTs, latestTs, historyLimitFlag)
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

	var notice string
	if hasMore {
		if rangeSet {
			notice = "older messages remain within the range — raise --limit"
		} else {
			notice = "older messages omitted — raise --limit or narrow with --since"
		}
	}

	return writeMessages(cmd, messages, resolver.resolve, notice, "")
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
