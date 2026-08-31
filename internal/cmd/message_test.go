package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/178inaba/slio/internal/slackclient"
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

func TestUserResolverPropagatesDeadlineExceeded(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","profile":{"display_name":"Alice"}}}`)
	}))
	t.Cleanup(srv.Close)

	client := slackclient.New("xoxp-test", slackclient.WithAPIURL(srv.URL+"/"))
	store, err := cache.Open("testkey")
	if err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done() // guarantee the deadline has already passed before resolving

	r := newUserResolver(ctx, client, store, time.Now())
	if got := r.resolve("U1"); got != "" {
		t.Errorf("resolve() = %q, want empty (falls back to the raw ID) while an error is recorded", got)
	}
	if r.err() == nil {
		t.Error("err() = nil, want a deadline-exceeded error to have been recorded")
	}
}

func TestUserResolverDoesNotRecordOrdinaryLookupFailure(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":false,"error":"user_not_found"}`)
	}))
	t.Cleanup(srv.Close)

	client := slackclient.New("xoxp-test", slackclient.WithAPIURL(srv.URL+"/"))
	store, err := cache.Open("testkey2")
	if err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}

	r := newUserResolver(context.Background(), client, store, time.Now())
	if got := r.resolve("U1"); got != "" {
		t.Errorf("resolve() = %q, want empty", got)
	}
	if err := r.err(); err != nil {
		t.Errorf("err() = %v, want nil for an ordinary (non-deadline) lookup failure", err)
	}
}
