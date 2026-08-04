package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/178inaba/slio/internal/config"
	"github.com/178inaba/slio/internal/format"
	"github.com/spf13/cobra"
)

// resolveWorkspace resolves credentials for a command invocation and, when
// SLIO_TOKEN bypassed profile resolution, calls auth.test once to learn
// the workspace host (for permalinks) and a cache key derived from its
// team ID (since no profile name is available to key the cache by).
// urlHost is the host parsed from the command's URL argument, or "" for
// commands that don't take one (search, channel list).
func resolveWorkspace(ctx context.Context, urlHost string) (creds config.Credentials, host, cacheKey string, err error) {
	file, err := config.Load()
	if err != nil {
		return config.Credentials{}, "", "", err
	}
	creds, err = config.Resolve(file, profileFlag, urlHost, os.Getenv)
	if err != nil {
		return config.Credentials{}, "", "", err
	}
	if !creds.ViaEnvToken {
		return creds, creds.Host, creds.Profile, nil
	}

	result, err := slackClientFactory(creds.Token).AuthTest(ctx)
	if err != nil {
		return config.Credentials{}, "", "", fmt.Errorf("resolve workspace for SLIO_TOKEN: %w", err)
	}
	return creds, result.Host, "team-" + result.TeamID, nil
}

// jsonMessagesEnvelope is the --format json shape for a list-of-messages
// command (history, search, thread): the rendered messages, plus a
// truncation notice when one applies.
type jsonMessagesEnvelope struct {
	Messages json.RawMessage `json:"messages"`
	Notice   string          `json:"notice,omitempty"`
}

// writeMessages renders messages per the --format flag and writes them to
// the command's output. leadingNotice/trailingNotice (either may be "")
// carry a truncation notice — "history" needs it to precede the message
// list ("older messages omitted"), "search" needs it to follow ("N more
// results"). In JSON mode there's no leading/trailing distinction (object
// field order doesn't matter to a consumer), so both collapse into a
// single "notice" field so the output stays valid, parseable JSON.
func writeMessages(cmd *cobra.Command, messages []format.Message, resolve format.Resolver, leadingNotice, trailingNotice string) error {
	out := cmd.OutOrStdout()

	if formatFlag == "json" {
		data, err := format.RenderJSON(messages, resolve)
		if err != nil {
			return err
		}
		envelope := jsonMessagesEnvelope{
			Messages: data,
			Notice:   strings.TrimSpace(leadingNotice + trailingNotice),
		}
		encoded, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(encoded))
		return err
	}

	if leadingNotice != "" {
		if _, err := fmt.Fprintln(out, leadingNotice); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, format.RenderMarkdownList(messages, resolve)); err != nil {
		return err
	}
	if trailingNotice != "" {
		if _, err := fmt.Fprintln(out, trailingNotice); err != nil {
			return err
		}
	}
	return nil
}
