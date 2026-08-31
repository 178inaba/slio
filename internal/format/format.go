// Package format renders slio's normalized Message model as either
// AI-readable Markdown or JSON. It performs no I/O: display names and
// mention resolution are supplied by the caller via Resolver, which is
// typically backed by internal/cache and users.info.
package format

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Format is the output format selected by --format. It implements
// pflag.Value, so a typo is rejected while cobra parses the flags — before
// the command runs and issues any Slack request.
type Format string

// Formats accepted by --format.
const (
	Markdown Format = "md"
	JSON     Format = "json"
)

func (f Format) String() string { return string(f) }

// Set assigns only on success, so a rejected value leaves the receiver — the
// flag's default — intact.
func (f *Format) Set(s string) error {
	switch v := Format(s); v {
	case Markdown, JSON:
		*f = v
		return nil
	}
	return unsupportedError(Format(s))
}

// Type names the value shown in the --format help line. It reports "string"
// rather than "format" because that help text is part of the agent-facing
// contract kept in sync across README.md, skills/slio/SKILL.md and the help
// strings; see CLAUDE.md.
func (Format) Type() string { return "string" }

// unsupportedError is shared by Set and by the callers that switch on a
// Format: a Format converted from an arbitrary string still type-checks, so
// a switch cannot assume its value went through Set. Keeping one constructor
// means the accepted values are listed in one place.
func unsupportedError(f Format) error {
	return fmt.Errorf("invalid --format %q: must be %q or %q", f, Markdown, JSON)
}

// Resolver maps a Slack user ID to its display name, used to expand
// <@U…> mentions in message text. An empty return means "unknown" and
// falls back to any inline display text Slack included, or the raw ID.
type Resolver func(userID string) string

// Reaction is one aggregated reaction on a message.
type Reaction struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// FileInfo describes a message attachment.
type FileInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	// LocalPath is set only when --download saved the file locally.
	LocalPath string `json:"local_path,omitempty"`
}

// Message is slio's normalized representation of a single Slack message,
// assembled by the command layer from either conversations.replies/history
// (which populate ReplyCount/ThreadPermalink) or search.messages (which
// populates Permalink instead — search.messages reports neither reaction
// nor reply-count data).
type Message struct {
	// Ts is the raw Slack timestamp (e.g. "1234567890.123456"), always
	// included in JSON output alongside the formatted time so agents can
	// build permalinks and follow-up calls.
	Ts   string
	Time time.Time

	// Author is a resolved user display name or a bot's
	// username/bot_profile.name; formatting treats both the same.
	Author string

	// Text is the raw mrkdwn message body, transformed by renderText.
	Text string

	Edited bool
	// IsSystem marks a join/leave/etc. message, rendered as a single line.
	IsSystem bool

	Reactions []Reaction
	Files     []FileInfo

	// ReplyCount and ThreadPermalink are set only for `history` output, on
	// messages that have replies.
	ReplyCount      int
	ThreadPermalink string

	// Permalink is set only for `search` output, taken directly from the
	// API response.
	Permalink string

	// QuotedBlocks holds bot attachment/block text extracted for
	// rendering as blockquotes.
	QuotedBlocks []string

	// Linked marks the one message a permalink pointed at, set only by
	// `thread` (the only command taking a single message's URL).
	Linked bool
}

// linkedMarker trails the header line of the message a permalink pointed
// at. It is a suffix rather than a prefix so the author and time stay where
// a reader — and a consumer diffing md output — expects them.
const linkedMarker = " 🎯 _linked message_"

var fencedCodeRe = regexp.MustCompile("(?s)```.*?```")

// renderText converts Slack mrkdwn to GitHub-flavored Markdown: mentions,
// channel/subteam references, and `<url|text>` links are expanded (via
// resolveUser for user mentions), *bold*/~strike~ become **bold**/~~strike~~,
// and HTML entities are unescaped. Fenced code blocks are left untouched
// (aside from entity unescaping) so their contents render exactly as
// written; _italic_ and quote lines already match GitHub Markdown, so they
// pass through unchanged.
func renderText(raw string, resolveUser Resolver) string {
	var b strings.Builder
	last := 0
	for _, loc := range fencedCodeRe.FindAllStringIndex(raw, -1) {
		b.WriteString(transformPlain(raw[last:loc[0]], resolveUser))
		b.WriteString(unescapeEntities(raw[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(transformPlain(raw[last:], resolveUser))
	return b.String()
}

var (
	mentionRe = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]*))?>`)
	channelRe = regexp.MustCompile(`<#([A-Z0-9]+)(?:\|([^>]*))?>`)
	subteamRe = regexp.MustCompile(`<!subteam\^([A-Z0-9]+)(?:\|([^>]*))?>`)
	specialRe = regexp.MustCompile(`<!(here|channel|everyone)>`)
	linkRe    = regexp.MustCompile(`<([^|<>]+)\|([^>]*)>`)
	// The boundary groups (never matching another *) keep these from
	// matching the inner single-* pair of text that's already **bold**
	// or ~~struck~~ (which would otherwise corrupt it into ***bold***).
	boldRe   = regexp.MustCompile(`(^|[^*])\*([^*\n]+)\*([^*]|$)`)
	strikeRe = regexp.MustCompile(`(^|[^~])~([^~\n]+)~([^~]|$)`)
)

// transformPlain applies Slack markup expansion to text known not to be
// inside a fenced code block. The order matters: mention/channel/subteam/
// special markup must be expanded before the generic link pattern (which
// would otherwise also match their "<...|...>" shape), and entities are
// unescaped only after all literal "<...>" markup has been consumed.
func transformPlain(s string, resolveUser Resolver) string {
	s = mentionRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mentionRe.FindStringSubmatch(m)
		id, fallback := sub[1], sub[2]
		if name := resolveUser(id); name != "" {
			return "**@" + name + "**"
		}
		if fallback != "" {
			return "**@" + fallback + "**"
		}
		return "**@" + id + "**"
	})
	s = channelRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := channelRe.FindStringSubmatch(m)
		id, name := sub[1], sub[2]
		if name == "" {
			return "#" + id
		}
		return "#" + name
	})
	s = subteamRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := subteamRe.FindStringSubmatch(m)
		id, name := sub[1], sub[2]
		if name == "" {
			return "**@" + id + "**"
		}
		return "**" + name + "**"
	})
	s = specialRe.ReplaceAllString(s, "@$1")
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		url, text := sub[1], sub[2]
		return "[" + text + "](" + url + ")"
	})
	s = unescapeEntities(s)
	s = boldRe.ReplaceAllString(s, "${1}**${2}**${3}")
	s = strikeRe.ReplaceAllString(s, "${1}~~${2}~~${3}")
	return s
}

func unescapeEntities(s string) string {
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s
}

// renderMarkdown renders a single message as a Markdown block.
func renderMarkdown(m Message, resolveUser Resolver) string {
	if m.IsSystem {
		// The marker goes outside the italics wrapping the whole line.
		line := fmt.Sprintf("_%s — %s_", formatLocalTime(m.Time), renderText(m.Text, resolveUser))
		if m.Linked {
			line += linkedMarker
		}
		return line + "\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** — %s", m.Author, formatLocalTime(m.Time))
	if m.Edited {
		b.WriteString(" (edited)")
	}
	if m.Linked {
		b.WriteString(linkedMarker)
	}
	b.WriteString("\n")
	b.WriteString(renderText(m.Text, resolveUser))
	b.WriteString("\n")

	for _, block := range m.QuotedBlocks {
		rendered := renderText(block, resolveUser)
		b.WriteString("\n> ")
		b.WriteString(strings.ReplaceAll(strings.TrimRight(rendered, "\n"), "\n", "\n> "))
		b.WriteString("\n")
	}

	if len(m.Reactions) > 0 {
		parts := make([]string, len(m.Reactions))
		for i, r := range m.Reactions {
			parts[i] = fmt.Sprintf(":%s: %d", r.Name, r.Count)
		}
		fmt.Fprintf(&b, "\n%s\n", strings.Join(parts, " "))
	}

	for _, f := range m.Files {
		if f.LocalPath != "" {
			fmt.Fprintf(&b, "\n📎 %s (%s, %s) — saved to %s\n", f.Name, f.Type, humanSize(f.Size), f.LocalPath)
		} else {
			fmt.Fprintf(&b, "\n📎 %s (%s, %s)\n", f.Name, f.Type, humanSize(f.Size))
		}
	}

	if m.ReplyCount > 0 && m.ThreadPermalink != "" {
		fmt.Fprintf(&b, "\n💬 %d %s — %s\n", m.ReplyCount, pluralize(m.ReplyCount), m.ThreadPermalink)
	}
	if m.Permalink != "" {
		fmt.Fprintf(&b, "\n🔗 %s\n", m.Permalink)
	}

	return b.String()
}

// renderMarkdownList renders a slice of messages, in the order given, as
// Markdown separated by horizontal rules.
func renderMarkdownList(messages []Message, resolveUser Resolver) string {
	parts := make([]string, len(messages))
	for i, m := range messages {
		parts[i] = renderMarkdown(m, resolveUser)
	}
	return strings.Join(parts, "\n---\n\n")
}

// jsonMessage is the --format json representation of a single message.
type jsonMessage struct {
	Ts              string     `json:"ts"`
	Time            time.Time  `json:"time"`
	Author          string     `json:"author,omitempty"`
	Text            string     `json:"text"`
	Edited          bool       `json:"edited,omitempty"`
	IsSystem        bool       `json:"is_system,omitempty"`
	Reactions       []Reaction `json:"reactions,omitempty"`
	Files           []FileInfo `json:"files,omitempty"`
	ReplyCount      int        `json:"reply_count,omitempty"`
	ThreadPermalink string     `json:"thread_permalink,omitempty"`
	Permalink       string     `json:"permalink,omitempty"`
	QuotedBlocks    []string   `json:"quoted_blocks,omitempty"`
	Linked          bool       `json:"linked,omitempty"`
}

// jsonMessagesEnvelope is the --format json shape for a list-of-messages
// command (history, search, thread): the rendered messages, plus a
// truncation notice when one applies.
type jsonMessagesEnvelope struct {
	Messages []jsonMessage `json:"messages"`
	Notice   string        `json:"notice,omitempty"`
}

// toJSONMessages converts messages, in the order given, to their JSON
// shape. Text (and QuotedBlocks) is rendered the same way as in Markdown
// output (mentions expanded, mrkdwn converted) so consumers don't need to
// understand Slack's markup.
//
// The result is always non-nil: an empty list has to encode as [] rather
// than null, which is what a caller ranging over "messages" expects.
func toJSONMessages(messages []Message, resolveUser Resolver) []jsonMessage {
	out := make([]jsonMessage, len(messages))
	for i, m := range messages {
		var quotedBlocks []string
		if len(m.QuotedBlocks) > 0 {
			quotedBlocks = make([]string, len(m.QuotedBlocks))
			for j, block := range m.QuotedBlocks {
				quotedBlocks[j] = renderText(block, resolveUser)
			}
		}
		out[i] = jsonMessage{
			Ts:              m.Ts,
			Time:            m.Time,
			Author:          m.Author,
			Text:            renderText(m.Text, resolveUser),
			Edited:          m.Edited,
			IsSystem:        m.IsSystem,
			Reactions:       m.Reactions,
			Files:           m.Files,
			ReplyCount:      m.ReplyCount,
			ThreadPermalink: m.ThreadPermalink,
			Permalink:       m.Permalink,
			QuotedBlocks:    quotedBlocks,
			Linked:          m.Linked,
		}
	}
	return out
}

// WriteMessages renders messages in the requested format and writes them
// to w. leadingNotice/trailingNotice (either may be "") carry a truncation
// notice — "history" needs it to precede the message list ("older messages
// omitted"), "search" needs it to follow ("N more results"). In JSON mode
// there's no leading/trailing distinction (object field order doesn't
// matter to a consumer), so both collapse into a single "notice" field so
// the output stays valid, parseable JSON.
func WriteMessages(w io.Writer, f Format, messages []Message, resolveUser Resolver, leadingNotice, trailingNotice string) error {
	switch f {
	case JSON:
		envelope := jsonMessagesEnvelope{
			Messages: toJSONMessages(messages, resolveUser),
			Notice:   strings.TrimSpace(leadingNotice + trailingNotice),
		}
		data, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	case Markdown:
		if leadingNotice != "" {
			if _, err := fmt.Fprintln(w, leadingNotice); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, renderMarkdownList(messages, resolveUser)); err != nil {
			return err
		}
		if trailingNotice != "" {
			if _, err := fmt.Fprintln(w, trailingNotice); err != nil {
				return err
			}
		}
		return nil
	default:
		return unsupportedError(f)
	}
}

// Channel is one channel in `channel list` output. Unlike Message it needs
// no rendering pass, so it doubles as the JSON shape rather than having a
// separate wire type.
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WriteChannels renders channels in the requested format and writes them
// to w.
func WriteChannels(w io.Writer, f Format, channels []Channel) error {
	switch f {
	case JSON:
		data, err := json.MarshalIndent(channels, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	case Markdown:
		for _, c := range channels {
			if _, err := fmt.Fprintf(w, "#%s\t%s\n", c.Name, c.ID); err != nil {
				return err
			}
		}
		return nil
	default:
		return unsupportedError(f)
	}
}

// ParseTs converts a Slack message ts (e.g. "1234567890.123456") to a
// time.Time.
func ParseTs(ts string) (time.Time, error) {
	sec, micro, ok := strings.Cut(ts, ".")
	if !ok {
		return time.Time{}, fmt.Errorf("invalid Slack ts %q: missing fractional part", ts)
	}
	secs, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid Slack ts %q: %w", ts, err)
	}
	micros, err := strconv.ParseInt(micro, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid Slack ts %q: %w", ts, err)
	}
	return time.Unix(secs, micros*1000), nil
}

// Ts converts a time.Time to a Slack ts string, for use as the
// oldest/latest API parameters.
func Ts(t time.Time) string {
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}

func formatLocalTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

func pluralize(n int) string {
	if n == 1 {
		return "reply"
	}
	return "replies"
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
