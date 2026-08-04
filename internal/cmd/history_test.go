package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunHistoryByChannelIDOldestToNewestOrder(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"newest","ts":"1.000002"},`+
				`{"type":"message","text":"oldest","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	testCmd := &cobra.Command{}
	var out bytes.Buffer
	testCmd.SetOut(&out)

	if err := runHistory(testCmd, []string{"C1"}); err != nil {
		t.Fatalf("runHistory() error = %v", err)
	}

	got := out.String()
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
			fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"a","ts":"1.000003"},`+
				`{"type":"message","text":"b","ts":"1.000002"},`+
				`{"type":"message","text":"c","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	historyLimitFlag = 2
	t.Cleanup(func() { historyLimitFlag = defaultHistoryLimit })

	testCmd := &cobra.Command{}
	var out bytes.Buffer
	testCmd.SetOut(&out)

	if err := runHistory(testCmd, []string{"C1"}); err != nil {
		t.Fatalf("runHistory() error = %v", err)
	}

	got := out.String()
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
			fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C42","name":"general"}]}`)
		},
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			historyChannelParam = r.FormValue("channel")
			fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"hi","ts":"1.000001"}],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	testCmd := &cobra.Command{}
	testCmd.SetOut(&bytes.Buffer{})

	if err := runHistory(testCmd, []string{"#general"}); err != nil {
		t.Fatalf("runHistory() error = %v", err)
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
			fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C42","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	testCmd := &cobra.Command{}
	testCmd.SetOut(&bytes.Buffer{})

	err := runHistory(testCmd, []string{"#nope"})
	if err == nil {
		t.Fatal("runHistory() error = nil, want error for unknown channel name")
	}
}

func TestRunHistoryJSONFormatIsValidWithNotice(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","text":"a","ts":"1.000002"},`+
				`{"type":"message","text":"b","ts":"1.000001"}`+
				`],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	historyLimitFlag = 1
	t.Cleanup(func() { historyLimitFlag = defaultHistoryLimit })
	formatFlag = "json"
	t.Cleanup(func() { formatFlag = "md" })

	testCmd := &cobra.Command{}
	var out bytes.Buffer
	testCmd.SetOut(&out)

	if err := runHistory(testCmd, []string{"C1"}); err != nil {
		t.Fatalf("runHistory() error = %v", err)
	}

	var envelope struct {
		Messages []map[string]any `json:"messages"`
		Notice   string           `json:"notice"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal output: %v; output = %s", err, out.String())
	}
	if envelope.Notice == "" {
		t.Error("Notice is empty, want a truncation notice")
	}
	if len(envelope.Messages) != 1 {
		t.Errorf("len(Messages) = %d, want 1", len(envelope.Messages))
	}
}
