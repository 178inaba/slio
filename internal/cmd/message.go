package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/178inaba/slio/internal/format"
	"github.com/178inaba/slio/internal/slackclient"
	"github.com/slack-go/slack"
)

// userResolver resolves Slack user IDs to display names, backed by the
// on-disk cache and falling back to users.info on a cache miss. It also
// memoizes lookups for the lifetime of a single command invocation, since
// the same author or mention commonly repeats across many messages.
type userResolver struct {
	ctx    context.Context
	client *slackclient.Client
	store  *cache.Store
	now    time.Time
	memo   map[string]string
}

func newUserResolver(ctx context.Context, client *slackclient.Client, store *cache.Store, now time.Time) *userResolver {
	return &userResolver{ctx: ctx, client: client, store: store, now: now, memo: map[string]string{}}
}

// resolve implements format.Resolver. A miss (network error included) is
// reported as "" so the caller falls back to raw IDs rather than failing
// the whole render.
func (r *userResolver) resolve(userID string) string {
	if userID == "" {
		return ""
	}
	if name, ok := r.memo[userID]; ok {
		return name
	}

	if name, ok, err := r.store.UserDisplayName(userID, r.now); err == nil && ok {
		r.memo[userID] = name
		return name
	}

	user, err := r.client.GetUserInfo(r.ctx, userID)
	if err != nil {
		return ""
	}
	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}
	if name == "" {
		name = user.Name
	}

	_ = r.store.PutUser(userID, name, r.now) // best-effort; a cache write failure shouldn't fail the render
	r.memo[userID] = name
	return name
}

// systemSubtypes are message subtypes rendered as a single system-message
// line rather than the full author/text block.
var systemSubtypes = map[string]bool{
	"channel_join":      true,
	"channel_leave":     true,
	"group_join":        true,
	"group_leave":       true,
	"channel_topic":     true,
	"channel_purpose":   true,
	"channel_name":      true,
	"channel_archive":   true,
	"channel_unarchive": true,
	"pinned_item":       true,
	"unpinned_item":     true,
}

// authorFor returns the display name to render for a message's sender:
// bot messages use their username/bot profile name (users.info can't
// resolve bot senders), everything else resolves the user ID.
func authorFor(m slack.Message, resolve func(string) string) string {
	if m.BotID != "" || m.SubType == "bot_message" {
		if m.Username != "" {
			return m.Username
		}
		if m.BotProfile != nil && m.BotProfile.Name != "" {
			return m.BotProfile.Name
		}
		return "bot"
	}
	if name := resolve(m.User); name != "" {
		return name
	}
	return m.User
}

// quotedBlocksFromMsg extracts a best-effort text rendering of bot
// attachments and section blocks, for display as blockquotes. Other block
// types (dividers, images, actions, rich text) are skipped rather than
// fully re-implemented.
func quotedBlocksFromMsg(m slack.Message) []string {
	var blocks []string
	for _, a := range m.Attachments {
		text := a.Text
		if text == "" {
			text = a.Fallback
		}
		if text != "" {
			blocks = append(blocks, text)
		}
	}
	for _, b := range m.Blocks.BlockSet {
		if section, ok := b.(*slack.SectionBlock); ok && section.Text != nil && section.Text.Text != "" {
			blocks = append(blocks, section.Text.Text)
		}
	}
	return blocks
}

// buildThreadPermalink constructs a thread permalink locally from a
// workspace host, channel ID, and ts, rather than calling
// chat.getPermalink per message (which would be an N+1 call against the
// 90s deadline).
func buildThreadPermalink(host, channel, ts string) string {
	return fmt.Sprintf("https://%s/archives/%s/p%s?thread_ts=%s&cid=%s",
		host, channel, strings.Replace(ts, ".", "", 1), ts, channel)
}

func filesFromMsg(files []slack.File) []format.FileInfo {
	out := make([]format.FileInfo, len(files))
	for i, f := range files {
		out[i] = format.FileInfo{Name: f.Name, Type: f.Filetype, Size: int64(f.Size)}
	}
	return out
}

func reactionsFromMsg(reactions []slack.ItemReaction) []format.Reaction {
	out := make([]format.Reaction, len(reactions))
	for i, r := range reactions {
		out[i] = format.Reaction{Name: r.Name, Count: r.Count}
	}
	return out
}

// messageFromMsg converts a slack.Message from conversations.replies or
// conversations.history into slio's normalized format.Message. Reply
// count/permalink are included only when withReplyInfo is set (`history`
// output; `thread` output doesn't need to point at itself).
func messageFromMsg(m slack.Message, host string, resolve func(string) string, withReplyInfo bool) (format.Message, error) {
	t, err := format.ParseTs(m.Timestamp)
	if err != nil {
		return format.Message{}, err
	}

	if systemSubtypes[m.SubType] {
		return format.Message{Ts: m.Timestamp, Time: t, Text: m.Text, IsSystem: true}, nil
	}

	out := format.Message{
		Ts:           m.Timestamp,
		Time:         t,
		Author:       authorFor(m, resolve),
		Text:         m.Text,
		Edited:       m.Edited != nil,
		Reactions:    reactionsFromMsg(m.Reactions),
		Files:        filesFromMsg(m.Files),
		QuotedBlocks: quotedBlocksFromMsg(m),
	}
	if withReplyInfo && m.ReplyCount > 0 {
		out.ReplyCount = m.ReplyCount
		out.ThreadPermalink = buildThreadPermalink(host, m.Channel, m.Timestamp)
	}
	return out, nil
}

// messageFromSearchMatch converts a slack.SearchMessage into slio's
// normalized format.Message. search.messages doesn't report reactions,
// files, edited state, or reply counts, so those fields are left zero;
// the permalink comes directly from the API response instead of being
// built locally.
func messageFromSearchMatch(m slack.SearchMessage, resolve func(string) string) (format.Message, error) {
	t, err := format.ParseTs(m.Timestamp)
	if err != nil {
		return format.Message{}, err
	}

	author := m.Username
	if author == "" {
		if name := resolve(m.User); name != "" {
			author = name
		} else {
			author = m.User
		}
	}

	var quoted []string
	for _, a := range m.Attachments {
		text := a.Text
		if text == "" {
			text = a.Fallback
		}
		if text != "" {
			quoted = append(quoted, text)
		}
	}
	for _, b := range m.Blocks.BlockSet {
		if section, ok := b.(*slack.SectionBlock); ok && section.Text != nil && section.Text.Text != "" {
			quoted = append(quoted, section.Text.Text)
		}
	}

	return format.Message{
		Ts:           m.Timestamp,
		Time:         t,
		Author:       author,
		Text:         m.Text,
		Permalink:    m.Permalink,
		QuotedBlocks: quoted,
	}, nil
}
