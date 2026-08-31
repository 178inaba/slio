package cmd

import (
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
		"conversations.history": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"newest","ts":"1.000002"},`+
				`{"type":"message","text":"oldest","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, stderr, code := runSlio(t, "history", "C1")
	if code != 0 {
		t.Fatalf("slio history: exit code = %d, stderr = %s", code, stderr)
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
		"conversations.history": func(w http.ResponseWriter, _ *http.Request) {
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

	got, stderr, code := runSlio(t, "history", "C1", "--limit", "2")
	if code != 0 {
		t.Fatalf("slio history --limit 2: exit code = %d, stderr = %s", code, stderr)
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
		"users.conversations": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C42","name":"general"}]}`)
		},
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			historyChannelParam = r.FormValue("channel")
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"hi","ts":"1.000001"}],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	if _, stderr, code := runSlio(t, "history", "#general"); code != 0 {
		t.Fatalf("slio history #general: exit code = %d, stderr = %s", code, stderr)
	}
	if historyChannelParam != "C42" {
		t.Errorf("history channel param = %q, want C42 (resolved from #general)", historyChannelParam)
	}
}

func TestRunHistoryUnknownChannelName(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C42","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	_, stderr, code := runSlio(t, "history", "#nope")
	if code == 0 {
		t.Fatal("slio history #nope: exit code = 0, want a failure for an unknown channel name")
	}
	if got := errorLine(t, stderr); !strings.Contains(got, "#nope") {
		t.Errorf("reported %q, want it to name the channel", got)
	}
}

func TestRunHistoryJSONFormatIsValidWithNotice(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.history": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"a","ts":"1.000002"},`+
				`{"type":"message","text":"b","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, stderr, code := runSlio(t, "history", "C1", "--limit", "1", "--format", "json")
	if code != 0 {
		t.Fatalf("slio history --format json: exit code = %d, stderr = %s", code, stderr)
	}

	envelope := decodeMessagesEnvelope(t, got)
	if envelope.Notice == "" {
		t.Error("Notice is empty, want a truncation notice")
	}
	if len(envelope.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(envelope.Messages))
	}
}
