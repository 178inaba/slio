package cmd

import (
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestAuthorForRegularUser(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{User: "U1"}}
	got := authorFor(m, func(id string) string {
		if id == "U1" {
			return "Alice"
		}
		return ""
	})
	if got != "Alice" {
		t.Errorf("authorFor() = %q, want Alice", got)
	}
}

func TestAuthorForUnresolvedUserFallsBackToID(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{User: "U1"}}
	got := authorFor(m, func(string) string { return "" })
	if got != "U1" {
		t.Errorf("authorFor() = %q, want U1", got)
	}
}

func TestAuthorForBotWithUsername(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{BotID: "B1", Username: "GitHub"}}
	got := authorFor(m, func(string) string { return "" })
	if got != "GitHub" {
		t.Errorf("authorFor() = %q, want GitHub", got)
	}
}

func TestAuthorForBotWithBotProfileName(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{BotID: "B1", BotProfile: &slack.BotProfile{Name: "CI Bot"}}}
	got := authorFor(m, func(string) string { return "" })
	if got != "CI Bot" {
		t.Errorf("authorFor() = %q, want CI Bot", got)
	}
}

func TestMessageFromMsgRegular(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{
		User:      "U1",
		Text:      "hello",
		Timestamp: "1234567890.123456",
		Channel:   "C1",
	}}
	got, err := messageFromMsg(m, "myws.slack.com", func(string) string { return "Alice" }, false)
	if err != nil {
		t.Fatalf("messageFromMsg() error = %v", err)
	}
	if got.Author != "Alice" || got.Text != "hello" || got.Ts != "1234567890.123456" {
		t.Errorf("messageFromMsg() = %+v, want Author=Alice Text=hello Ts=1234567890.123456", got)
	}
	if got.ThreadPermalink != "" {
		t.Errorf("ThreadPermalink = %q, want empty when withReplyInfo=false", got.ThreadPermalink)
	}
}

func TestMessageFromMsgWithReplyInfo(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{
		User:       "U1",
		Text:       "hello",
		Timestamp:  "1234567890.123456",
		Channel:    "C1",
		ReplyCount: 3,
	}}
	got, err := messageFromMsg(m, "myws.slack.com", func(string) string { return "Alice" }, true)
	if err != nil {
		t.Fatalf("messageFromMsg() error = %v", err)
	}
	if got.ReplyCount != 3 {
		t.Errorf("ReplyCount = %d, want 3", got.ReplyCount)
	}
	want := "https://myws.slack.com/archives/C1/p1234567890123456?thread_ts=1234567890.123456&cid=C1"
	if got.ThreadPermalink != want {
		t.Errorf("ThreadPermalink = %q, want %q", got.ThreadPermalink, want)
	}
}

func TestMessageFromMsgSystemMessage(t *testing.T) {
	m := slack.Message{Msg: slack.Msg{
		Text:      "Alice has joined the channel",
		Timestamp: "1234567890.123456",
		SubType:   "channel_join",
	}}
	got, err := messageFromMsg(m, "myws.slack.com", func(string) string { return "" }, false)
	if err != nil {
		t.Fatalf("messageFromMsg() error = %v", err)
	}
	if !got.IsSystem {
		t.Error("IsSystem = false, want true for channel_join")
	}
	if !strings.Contains(got.Text, "joined") {
		t.Errorf("Text = %q, want it to contain the join message", got.Text)
	}
}

func TestMessageFromSearchMatch(t *testing.T) {
	m := slack.SearchMessage{
		User:      "U1",
		Text:      "hello",
		Timestamp: "1234567890.123456",
		Permalink: "https://myws.slack.com/archives/C1/p1234567890123456",
	}
	got, err := messageFromSearchMatch(m, func(string) string { return "Alice" })
	if err != nil {
		t.Fatalf("messageFromSearchMatch() error = %v", err)
	}
	if got.Author != "Alice" || got.Permalink != m.Permalink {
		t.Errorf("messageFromSearchMatch() = %+v, want Author=Alice Permalink=%q", got, m.Permalink)
	}
}

func TestMessageFromSearchMatchBotUsesUsername(t *testing.T) {
	m := slack.SearchMessage{
		Username:  "GitHub",
		Text:      "build failed",
		Timestamp: "1234567890.123456",
	}
	got, err := messageFromSearchMatch(m, func(string) string { return "" })
	if err != nil {
		t.Fatalf("messageFromSearchMatch() error = %v", err)
	}
	if got.Author != "GitHub" {
		t.Errorf("Author = %q, want GitHub", got.Author)
	}
}

func TestBuildThreadPermalink(t *testing.T) {
	got := buildThreadPermalink("myws.slack.com", "C1", "1234567890.123456")
	want := "https://myws.slack.com/archives/C1/p1234567890123456?thread_ts=1234567890.123456&cid=C1"
	if got != want {
		t.Errorf("buildThreadPermalink() = %q, want %q", got, want)
	}
}
