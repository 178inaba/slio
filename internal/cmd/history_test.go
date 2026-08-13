package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunHistoryByChannelIDOldestToNewestOrder(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"newest","ts":"1.000002"},`+
				`{"type":"message","text":"oldest","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, _, err := runSlio(t, "history", "C1")
	if err != nil {
		t.Fatalf("slio history: %v", err)
	}

	oldestIdx := strings.Index(got, "oldest")
	newestIdx := strings.Index(got, "newest")
	if oldestIdx == -1 || newestIdx == -1 || oldestIdx > newestIdx {
		t.Errorf("output = %q, want oldest to appear before newest", got)
	}
}

func TestRunHistoryTruncationNoticeLeadsOutput(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			// limit+1 (3) messages in one page, has_more:false: the API
			// happened to have exactly that many on hand, and it's up to
			// ConversationHistory to notice len(all) > limit and report
			// hasMore itself.
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"a","ts":"1.000003"},`+
				`{"type":"message","text":"b","ts":"1.000002"},`+
				`{"type":"message","text":"c","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, _, err := runSlio(t, "history", "C1", "--limit", "2")
	if err != nil {
		t.Fatalf("slio history --limit 2: %v", err)
	}

	noticeIdx := strings.Index(got, "older messages omitted")
	textIdx := strings.Index(got, "**") // first rendered message block
	if noticeIdx == -1 {
		t.Fatalf("output = %q, want a truncation notice", got)
	}
	if textIdx != -1 && noticeIdx > textIdx {
		t.Errorf("output = %q, want the notice to precede the message list", got)
	}
}

func TestRunHistoryByNameResolvesViaCache(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var historyChannelParam string
	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C42","name":"general"}]}`)
		},
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			historyChannelParam = r.FormValue("channel")
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"hi","ts":"1.000001"}],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	if _, _, err := runSlio(t, "history", "#general"); err != nil {
		t.Fatalf("slio history #general: %v", err)
	}
	if historyChannelParam != "C42" {
		t.Errorf("history channel param = %q, want C42 (resolved from #general)", historyChannelParam)
	}
}

func TestRunHistoryUnknownChannelName(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C42","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	if _, _, err := runSlio(t, "history", "#nope"); err == nil {
		t.Fatal("slio history #nope: error = nil, want error for unknown channel name")
	}
}

func TestRunHistoryJSONFormatIsValidWithNotice(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"a","ts":"1.000002"},`+
				`{"type":"message","text":"b","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, _, err := runSlio(t, "history", "C1", "--limit", "1", "--format", "json")
	if err != nil {
		t.Fatalf("slio history --format json: %v", err)
	}

	var envelope struct {
		Messages []map[string]any `json:"messages"`
		Notice   string           `json:"notice"`
	}
	if err := json.Unmarshal([]byte(got), &envelope); err != nil {
		t.Fatalf("unmarshal output: %v; output = %s", err, got)
	}
	if envelope.Notice == "" {
		t.Error("Notice is empty, want a truncation notice")
	}
	if len(envelope.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(envelope.Messages))
	}
}
