package format

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func resolverFromMap(m map[string]string) Resolver {
	return func(id string) string { return m[id] }
}

func TestRenderTextMentionResolved(t *testing.T) {
	got := RenderText("<@U1> hello", resolverFromMap(map[string]string{"U1": "Alice"}))
	want := "@Alice hello"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextMentionUnresolvedWithFallback(t *testing.T) {
	got := RenderText("<@U1|bob>", resolverFromMap(nil))
	want := "@bob"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextMentionUnresolvedNoFallback(t *testing.T) {
	got := RenderText("<@U1>", resolverFromMap(nil))
	want := "@U1"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextChannelWithName(t *testing.T) {
	got := RenderText("<#C1|general>", resolverFromMap(nil))
	if got != "#general" {
		t.Errorf("RenderText() = %q, want #general", got)
	}
}

func TestRenderTextChannelWithoutName(t *testing.T) {
	got := RenderText("<#C1>", resolverFromMap(nil))
	if got != "#C1" {
		t.Errorf("RenderText() = %q, want #C1", got)
	}
}

func TestRenderTextSpecialMentions(t *testing.T) {
	got := RenderText("<!here> <!channel> <!everyone>", resolverFromMap(nil))
	want := "@here @channel @everyone"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextSubteamWithName(t *testing.T) {
	got := RenderText("<!subteam^S1|@eng>", resolverFromMap(nil))
	if got != "@eng" {
		t.Errorf("RenderText() = %q, want @eng", got)
	}
}

func TestRenderTextSubteamWithoutName(t *testing.T) {
	got := RenderText("<!subteam^S1>", resolverFromMap(nil))
	if got != "@S1" {
		t.Errorf("RenderText() = %q, want @S1", got)
	}
}

func TestRenderTextLink(t *testing.T) {
	got := RenderText("<https://example.com|Example>", resolverFromMap(nil))
	want := "[Example](https://example.com)"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextBareAutolinkUnchanged(t *testing.T) {
	raw := "<https://example.com>"
	got := RenderText(raw, resolverFromMap(nil))
	if got != raw {
		t.Errorf("RenderText() = %q, want unchanged %q", got, raw)
	}
}

func TestRenderTextBoldAndStrike(t *testing.T) {
	got := RenderText("*bold* and ~strike~", resolverFromMap(nil))
	want := "**bold** and ~~strike~~"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextItalicUnchanged(t *testing.T) {
	raw := "_italic_"
	got := RenderText(raw, resolverFromMap(nil))
	if got != raw {
		t.Errorf("RenderText() = %q, want unchanged %q", got, raw)
	}
}

func TestRenderTextEntityUnescape(t *testing.T) {
	got := RenderText("a &lt;b&gt; &amp; c", resolverFromMap(nil))
	want := "a <b> & c"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderTextFencedCodeBlockUntouchedExceptEntities(t *testing.T) {
	raw := "before\n```\n*not bold* &lt;tag&gt;\n```\nafter"
	got := RenderText(raw, resolverFromMap(nil))
	want := "before\n```\n*not bold* <tag>\n```\nafter"
	if got != want {
		t.Errorf("RenderText() = %q, want %q", got, want)
	}
}

func TestRenderMarkdownNormalMessage(t *testing.T) {
	m := Message{
		Ts:              "1234567890.123456",
		Time:            time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		Author:          "Alice",
		Text:            "hello <@U2>",
		Edited:          true,
		Reactions:       []Reaction{{Name: "+1", Count: 2}},
		Files:           []FileInfo{{Name: "report.pdf", Type: "pdf", Size: 1024}},
		ReplyCount:      3,
		ThreadPermalink: "https://myws.slack.com/archives/C1/p1234567890123456?thread_ts=1234567890.123456&cid=C1",
	}
	got := RenderMarkdown(m, resolverFromMap(map[string]string{"U2": "Bob"}))

	for _, want := range []string{
		"Alice", m.Time.Local().Format("2006-01-02 15:04"), "(edited)", "hello @Bob",
		":+1: 2", "report.pdf", "3 replies",
		"https://myws.slack.com/archives/C1/p1234567890123456?thread_ts=1234567890.123456&cid=C1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderMarkdown() = %q, want it to contain %q", got, want)
		}
	}
}

func TestRenderMarkdownSearchMessageShowsPermalinkNotReplyCount(t *testing.T) {
	m := Message{
		Ts:        "1234567890.123456",
		Time:      time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		Author:    "Alice",
		Text:      "hello",
		Permalink: "https://myws.slack.com/archives/C1/p1234567890123456",
	}
	got := RenderMarkdown(m, resolverFromMap(nil))

	if !strings.Contains(got, "https://myws.slack.com/archives/C1/p1234567890123456") {
		t.Errorf("RenderMarkdown() = %q, want it to contain the permalink", got)
	}
	if strings.Contains(got, "replies") || strings.Contains(got, "reply") {
		t.Errorf("RenderMarkdown() = %q, want no reply-count mention", got)
	}
}

func TestRenderMarkdownSystemMessageIsOneLine(t *testing.T) {
	m := Message{
		Ts:       "1234567890.123456",
		Time:     time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		Text:     "Alice has joined the channel",
		IsSystem: true,
	}
	got := RenderMarkdown(m, resolverFromMap(nil))

	trimmed := strings.TrimRight(got, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Errorf("RenderMarkdown() = %q, want a single line for a system message", got)
	}
	if !strings.Contains(got, "Alice has joined the channel") {
		t.Errorf("RenderMarkdown() = %q, want it to contain the system message text", got)
	}
}

func TestRenderJSONIncludesRawTsAndRendersText(t *testing.T) {
	messages := []Message{
		{
			Ts:     "1234567890.123456",
			Time:   time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
			Author: "Alice",
			Text:   "hi <@U2>",
		},
	}
	data, err := RenderJSON(messages, resolverFromMap(map[string]string{"U2": "Bob"}))
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal RenderJSON() output: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded length = %d, want 1", len(decoded))
	}
	if decoded[0]["ts"] != "1234567890.123456" {
		t.Errorf("ts = %v, want raw Slack ts", decoded[0]["ts"])
	}
	if decoded[0]["text"] != "hi @Bob" {
		t.Errorf("text = %v, want rendered text with mention expanded", decoded[0]["text"])
	}
	if _, ok := decoded[0]["reactions"]; ok {
		t.Errorf("reactions key present = %v, want omitted for an empty slice", decoded[0]["reactions"])
	}
}

func TestParseTsAndFormatTsRoundTrip(t *testing.T) {
	const ts = "1234567890.123456"
	tm, err := ParseTs(ts)
	if err != nil {
		t.Fatalf("ParseTs() error = %v", err)
	}
	if got := FormatTs(tm); got != ts {
		t.Errorf("FormatTs(ParseTs(%q)) = %q, want %q", ts, got, ts)
	}
}

func TestParseTsInvalid(t *testing.T) {
	if _, err := ParseTs("not-a-ts"); err == nil {
		t.Fatal("ParseTs() error = nil, want error")
	}
}
