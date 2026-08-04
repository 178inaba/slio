package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	// fatalErr holds the first deadline/cancellation error seen by
	// resolve, if any. Callers must check err() after rendering and
	// before writing output: a users.info failure that's merely
	// "not found" falls back to the raw ID as before, but a deadline
	// exceeded mid-resolution must not be swallowed into a degraded
	// success — that would violate "no partial-success output" on
	// deadline.
	fatalErr error
}

func newUserResolver(ctx context.Context, client *slackclient.Client, store *cache.Store, now time.Time) *userResolver {
	return &userResolver{ctx: ctx, client: client, store: store, now: now, memo: map[string]string{}}
}

// resolve implements format.Resolver. A miss is reported as "" so the
// caller falls back to raw IDs rather than failing the whole render — with
// one exception: a deadline/cancellation error is recorded (see err())
// rather than silently swallowed, since it means the invocation ran out of
// time, not that the user simply isn't resolvable.
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
		if r.fatalErr == nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
			r.fatalErr = fmt.Errorf("resolve user %s: %w", userID, err)
		}
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

// err returns the first deadline/cancellation error resolve encountered,
// or nil. Command RunE functions must check this after building messages
// and before writing output.
func (r *userResolver) err() error {
	return r.fatalErr
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

// quotedBlocksFrom extracts a best-effort text rendering of bot
// attachments and section blocks, for display as blockquotes. Other block
// types (dividers, images, actions, rich text) are skipped rather than
// fully re-implemented. Shared by messageFromMsg and messageFromSearchMatch:
// slack.Message and slack.SearchMessage carry identically-typed
// Attachments/Blocks fields.
func quotedBlocksFrom(attachments []slack.Attachment, blocks slack.Blocks) []string {
	var quoted []string
	for _, a := range attachments {
		text := a.Text
		if text == "" {
			text = a.Fallback
		}
		if text != "" {
			quoted = append(quoted, text)
		}
	}
	for _, b := range blocks.BlockSet {
		if section, ok := b.(*slack.SectionBlock); ok && section.Text != nil && section.Text.Text != "" {
			quoted = append(quoted, section.Text.Text)
		}
	}
	return quoted
}

// buildThreadPermalink constructs a thread permalink locally from a
// workspace host, channel ID, and ts, rather than calling
// chat.getPermalink per message (which would be an N+1 call against the
// 90s deadline).
func buildThreadPermalink(host, channel, ts string) string {
	return fmt.Sprintf("https://%s/archives/%s/p%s?thread_ts=%s&cid=%s",
		host, channel, strings.Replace(ts, ".", "", 1), ts, channel)
}

// downloadFiles downloads each file's contents into destDir, returning
// format.FileInfo entries with LocalPath set so the output tells the agent
// where to read them. Files with no URLPrivate (never observed in
// practice, but the field is optional in the API) are reported as
// metadata only, matching the default (non-download) rendering.
func downloadFiles(ctx context.Context, client *slackclient.Client, destDir string, files []slack.File) ([]format.FileInfo, error) {
	out := filesFromMsg(files)
	for i, f := range files {
		if f.URLPrivate == "" {
			continue
		}

		dest := filepath.Join(destDir, f.ID+"-"+filepath.Base(f.Name))
		if err := client.DownloadFile(ctx, f.URLPrivate, dest); err != nil {
			return nil, fmt.Errorf("download attachment %s: %w", f.Name, err)
		}
		out[i].LocalPath = dest
	}
	return out, nil
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
		QuotedBlocks: quotedBlocksFrom(m.Attachments, m.Blocks),
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

	return format.Message{
		Ts:           m.Timestamp,
		Time:         t,
		Author:       author,
		Text:         m.Text,
		Permalink:    m.Permalink,
		QuotedBlocks: quotedBlocksFrom(m.Attachments, m.Blocks),
	}, nil
}
