package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRunSearchPassesQueryThroughUnchanged(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var gotQuery string
	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"search.messages": func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err == nil {
				gotQuery = r.Form.Get("query")
			}
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":{"matches":[`+
				`{"text":"hi","ts":"1.000001","permalink":"https://myws.slack.com/archives/C1/p1000001"}`+
				`],"total":1,"pagination":{"page_count":1}}}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	root, out, _ := newTestRoot(t)
	query := "in:#general from:@someone hello"
	root.SetArgs([]string{"search", query})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	unescaped, err := url.QueryUnescape(gotQuery)
	if err != nil {
		t.Fatalf("QueryUnescape() error = %v", err)
	}
	if unescaped != query {
		t.Errorf("query sent = %q, want %q", unescaped, query)
	}
	if !strings.Contains(out.String(), "hi") {
		t.Errorf("output = %q, want it to contain the match text", out.String())
	}
}

func TestRunSearchTrailingMoreResultsNotice(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"search.messages": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":{"matches":[`+
				`{"text":"hi","ts":"1.000001","permalink":"https://myws.slack.com/archives/C1/p1000001"}`+
				`],"total":5,"pagination":{"page_count":1}}}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	root, out, _ := newTestRoot(t)
	root.SetArgs([]string{"search", "hello", "--limit", "1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := out.String()
	noticeIdx := strings.Index(got, "more results")
	textIdx := strings.LastIndex(got, "hi\n")
	if noticeIdx == -1 {
		t.Fatalf("output = %q, want a trailing more-results notice", got)
	}
	if textIdx != -1 && noticeIdx < textIdx {
		t.Errorf("output = %q, want the notice to follow the message list", got)
	}
	if !strings.Contains(got, "4 more results") {
		t.Errorf("output = %q, want it to say 4 more results (5 total - 1 shown)", got)
	}
}
