package cmd

import (
	"errors"
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/178inaba/slio/internal/format"
	"github.com/178inaba/slio/internal/parse"
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
	ref, err := parse.ThreadURL(args[0])
	if err != nil {
		return err
	}

	ctx, cancel := commandContext()
	defer cancel()

	creds, host, cacheKey, err := resolveWorkspace(ctx, ref.Host)
	if err != nil {
		return err
	}
	client := slackClientFactory(creds.Token)

	store, err := cache.Open(cacheKey)
	if err != nil {
		return err
	}

	msgs, err := client.ConversationReplies(ctx, ref.Channel, ref.Ts)
	if err != nil {
		return err
	}

	if threadDownloadFlag {
		return errors.New("thread --download: not implemented yet")
	}

	resolver := newUserResolver(ctx, client, store, time.Now())
	messages := make([]format.Message, 0, len(msgs))
	for _, m := range msgs {
		fm, err := messageFromMsg(m, host, resolver.resolve, false)
		if err != nil {
			return err
		}
		messages = append(messages, fm)
	}

	return writeMessages(cmd, messages, resolver.resolve, "", "")
}
