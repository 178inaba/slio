package slackclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"url":"https://myws.slack.com/","team":"My WS","user":"grace","team_id":"T123","user_id":"U123"}`)
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
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, `{"ok":false,"error":"invalid_auth"}`)
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
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"ok":false,"error":"ratelimited"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"url":"https://myws.slack.com/","team_id":"T123"}`)
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
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1234567890.000001"}],`+
				`"has_more":true,"response_metadata":{"next_cursor":"cursor1"}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","user":"U1","text":"reply","ts":"1234567890.000002"}],`+
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
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","name":"alice","real_name":"Alice Example","profile":{"display_name":"Alice"}}}`)
	})

	got, err := c.GetUserInfo(context.Background(), "U1")
	if err != nil {
		t.Fatalf("GetUserInfo() error = %v", err)
	}
	if got.Profile.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q, want Alice", got.Profile.DisplayName)
	}
}

func TestAuthTestDeadlineExceededWhileRateLimited(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"ok":false,"error":"ratelimited"}`)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.AuthTest(ctx)
	if err == nil {
		t.Fatal("AuthTest() error = nil, want deadline-exceeded error")
	}
}
