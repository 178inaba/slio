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

func newThreadCmd(g *globalFlags) *cobra.Command {
	var (
		download  bool
		outFormat format.Format
	)

	cmd := &cobra.Command{
		Use:   "thread <url>",
		Short: "Fetch a full thread by its Slack message permalink",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runThread(cmd, args, g, download, outFormat)
		},
	}
	cmd.Flags().BoolVar(&download, "download", false,
		"download attachments to a local temp directory and print their paths")
	addFormatFlag(cmd, &outFormat)

	return cmd
}

func runThread(cmd *cobra.Command, args []string, g *globalFlags, download bool, outFormat format.Format) error {
	ref, err := parse.ThreadURL(args[0])
	if err != nil {
		return err
	}

	ctx, cancel := commandContext(cmd, g.timeout)
	defer cancel()

	creds, host, cacheKey, err := resolveWorkspace(ctx, g.profile, ref.Host)
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
	if download {
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
		if download && len(m.Files) > 0 {
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

	return writeMessages(cmd, outFormat, messages, resolver.resolve, "", "")
}
