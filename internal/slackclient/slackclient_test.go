package slackclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("xoxp-test", WithAPIURL(srv.URL+"/"))
}

func TestAuthTestSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"url":"https://myws.slack.com/","team":"My WS","user":"grace","team_id":"T123","user_id":"U123"}`)
	})

	got, err := c.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("AuthTest() error = %v", err)
	}
	if got.Host != "myws.slack.com" {
		t.Errorf("Host = %q, want myws.slack.com", got.Host)
	}
	if got.TeamID != "T123" {
		t.Errorf("TeamID = %q, want T123", got.TeamID)
	}
}

func TestAuthTestNonRetryableError(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = fmt.Fprint(w, `{"ok":false,"error":"invalid_auth"}`)
	})

	_, err := c.AuthTest(context.Background())
	if err == nil {
		t.Fatal("AuthTest() error = nil, want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on non-rate-limit error)", got)
	}
}

func TestAuthTestRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, `{"ok":false,"error":"ratelimited"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"url":"https://myws.slack.com/","team_id":"T123"}`)
	})

	got, err := c.AuthTest(context.Background())
	if err != nil {
		t.Fatalf("AuthTest() error = %v", err)
	}
	if got.Host != "myws.slack.com" {
		t.Errorf("Host = %q, want myws.slack.com", got.Host)
	}
	if calls := atomic.LoadInt32(&calls); calls != 2 {
		t.Errorf("calls = %d, want 2 (one 429 then a retry)", calls)
	}
}

func TestConversationRepliesFollowsPagination(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1234567890.000001"}],`+
				`"has_more":true,"response_metadata":{"next_cursor":"cursor1"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","user":"U1","text":"reply","ts":"1234567890.000002"}],`+
			`"has_more":false,"response_metadata":{"next_cursor":""}}`)
	})

	msgs, err := c.ConversationReplies(context.Background(), "C1", "1234567890.000001")
	if err != nil {
		t.Fatalf("ConversationReplies() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].Text != "parent" || msgs[1].Text != "reply" {
		t.Errorf("msgs = %+v, want [parent reply]", msgs)
	}
	if calls := atomic.LoadInt32(&calls); calls != 2 {
		t.Errorf("calls = %d, want 2 (one page then a follow-up)", calls)
	}
}

func TestGetUserInfoSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","name":"alice","real_name":"Alice Example","profile":{"display_name":"Alice"}}}`)
	})

	got, err := c.GetUserInfo(context.Background(), "U1")
	if err != nil {
		t.Fatalf("GetUserInfo() error = %v", err)
	}
	if got.Profile.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q, want Alice", got.Profile.DisplayName)
	}
}

func TestConversationHistoryNoTruncation(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"a","ts":"1.000001"},{"type":"message","text":"b","ts":"1.000002"}],"has_more":false}`)
	})

	msgs, hasMore, err := c.ConversationHistory(context.Background(), "C1", "", "", 5)
	if err != nil {
		t.Fatalf("ConversationHistory() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if hasMore {
		t.Error("hasMore = true, want false")
	}
}

func TestConversationHistoryTruncatesAndReportsHasMore(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"a","ts":"1.000001"},{"type":"message","text":"b","ts":"1.000002"}],`+
				`"has_more":true,"response_metadata":{"next_cursor":"cursor1"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"c","ts":"1.000003"}],"has_more":false}`)
	})

	msgs, hasMore, err := c.ConversationHistory(context.Background(), "C1", "", "", 2)
	if err != nil {
		t.Fatalf("ConversationHistory() error = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2 (truncated to limit)", len(msgs))
	}
	if !hasMore {
		t.Error("hasMore = false, want true")
	}
}

func TestConversationHistoryClampsPerPageLimitTo999(t *testing.T) {
	var gotLimit string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.FormValue("limit")
		_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"a","ts":"1.000001"}],"has_more":false}`)
	})

	if _, _, err := c.ConversationHistory(context.Background(), "C1", "", "", 2000); err != nil {
		t.Fatalf("ConversationHistory() error = %v", err)
	}
	if gotLimit != "999" {
		t.Errorf("limit param = %q, want clamped to conversations.history's documented max of 999", gotLimit)
	}
}

func TestConversationsForUserFollowsPagination(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}],"response_metadata":{"next_cursor":"cursor1"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C2","name":"random"}],"response_metadata":{"next_cursor":""}}`)
	})

	channels, err := c.ConversationsForUser(context.Background())
	if err != nil {
		t.Fatalf("ConversationsForUser() error = %v", err)
	}
	if len(channels) != 2 || channels[0].Name != "general" || channels[1].Name != "random" {
		t.Errorf("channels = %+v, want [general random]", channels)
	}
}

func TestSearchMessagesReturnsMatchesAndTotal(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"messages":{"matches":[{"text":"hello","ts":"1.000001","permalink":"https://myws.slack.com/archives/C1/p1000001"}],"total":1}}`)
	})

	matches, total, err := c.SearchMessages(context.Background(), "hello", 20)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(matches) != 1 || matches[0].Text != "hello" {
		t.Errorf("matches = %+v, want one match with text hello", matches)
	}
}

func TestSearchMessagesKeepsPageSizeFixedAcrossPages(t *testing.T) {
	// A regression test for shrinking count as limit is approached: Slack's
	// "page" is defined relative to count, so a page 2 requested with a
	// different count than page 1 used is not "the next 100-sized page" —
	// it silently duplicates or skips results. count must stay fixed.
	var gotCounts []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotCounts = append(gotCounts, r.FormValue("count"))
		if r.FormValue("page") == "1" {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":{"matches":[{"text":"a","ts":"1.000001"}],"total":150,"pagination":{"page_count":2}}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"messages":{"matches":[{"text":"b","ts":"1.000002"}],"total":150,"pagination":{"page_count":2}}}`)
	})

	_, total, err := c.SearchMessages(context.Background(), "hello", 150)
	if err != nil {
		t.Fatalf("SearchMessages() error = %v", err)
	}
	if total != 150 {
		t.Errorf("total = %d, want 150", total)
	}
	if len(gotCounts) != 2 {
		t.Fatalf("requests made = %d, want 2 (one per page)", len(gotCounts))
	}
	for i, cnt := range gotCounts {
		if cnt != "100" {
			t.Errorf("count param on request %d = %q, want fixed 100 across all pages", i+1, cnt)
		}
	}
}

func TestDownloadFileSuccess(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "file contents")
	}))
	t.Cleanup(srv.Close)
	c := New("xoxp-test")

	dest := filepath.Join(t.TempDir(), "sub", "report.txt")
	if err := c.DownloadFile(context.Background(), srv.URL+"/files-pri/T1-F1/report.txt", dest); err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "file contents" {
		t.Errorf("downloaded content = %q, want %q", data, "file contents")
	}
	if gotAuth != "Bearer xoxp-test" {
		t.Errorf("Authorization header = %q, want Bearer xoxp-test", gotAuth)
	}
}

func TestDownloadFileDetectsSignInHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<!DOCTYPE html><html>sign in</html>")
	}))
	t.Cleanup(srv.Close)
	c := New("xoxp-test")

	dest := filepath.Join(t.TempDir(), "report.txt")
	err := c.DownloadFile(context.Background(), srv.URL+"/files-pri/T1-F1/report.txt", dest)
	if err == nil {
		t.Fatal("DownloadFile() error = nil, want error for an HTML sign-in response")
	}
	if !strings.Contains(err.Error(), "files:read") {
		t.Errorf("error = %v, want mention of the files:read scope", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Error("destination file exists, want nothing written for an HTML response")
	}
}

func TestDownloadFileRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)
	c := New("xoxp-test")

	dest := filepath.Join(t.TempDir(), "report.txt")
	if err := c.DownloadFile(context.Background(), srv.URL+"/files-pri/T1-F1/report.txt", dest); err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("calls = %d, want 2 (one 429 then a retry)", got)
	}
}

func TestAuthTestDeadlineExceededWhileRateLimited(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"ok":false,"error":"ratelimited"}`)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.AuthTest(ctx)
	if err == nil {
		t.Fatal("AuthTest() error = nil, want deadline-exceeded error")
	}
}
