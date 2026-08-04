package cmd

import (
	"fmt"
	"os"
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

	var downloadDir string
	if threadDownloadFlag {
		downloadDir, err = os.MkdirTemp("", "slio-thread-")
		if err != nil {
			return fmt.Errorf("create download directory: %w", err)
		}
	}

	resolver := newUserResolver(ctx, client, store, time.Now())
	messages := make([]format.Message, 0, len(msgs))
	for _, m := range msgs {
		fm, err := messageFromMsg(m, host, resolver.resolve, false)
		if err != nil {
			return err
		}
		if threadDownloadFlag && len(m.Files) > 0 {
			files, err := downloadFiles(ctx, client, downloadDir, m.Files)
			if err != nil {
				return err
			}
			fm.Files = files
		}
		messages = append(messages, fm)
	}
	if err := resolver.err(); err != nil {
		return err
	}

	return writeMessages(cmd, messages, resolver.resolve, "", "")
}
