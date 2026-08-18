package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/config"
)

func newSlackAPIMux(handlers map[string]http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc("/"+path, h)
	}
	return mux
}

func seedProfile(t *testing.T, host string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws": {Token: "xoxp-1", Host: host, TeamID: "T1"},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}
}

func TestRunThreadRendersFullThread(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.replies": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","user":"U1","text":"parent message","ts":"1234567890.000001","channel":"C1","reply_count":1},`+
				`{"type":"message","user":"U1","text":"a reply","ts":"1234567890.000002","channel":"C1"}`+
				`],"has_more":false,"response_metadata":{"next_cursor":""}}`)
		},
		"users.info": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","profile":{"display_name":"Alice"}}}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, stderr, code := runSlio(t, "thread", "https://myws.slack.com/archives/C1/p1234567890000001")
	if code != 0 {
		t.Fatalf("slio thread: exit code = %d, stderr = %s", code, stderr)
	}

	if !strings.Contains(got, "Alice") {
		t.Errorf("output = %q, want it to contain the resolved author Alice", got)
	}
	if !strings.Contains(got, "parent message") || !strings.Contains(got, "a reply") {
		t.Errorf("output = %q, want both the parent and the reply", got)
	}
}

func TestRunThreadUnregisteredWorkspace(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	_, stderr, code := runSlio(t, "thread", "https://unknown.slack.com/archives/C1/p1234567890000001")
	if code == 0 {
		t.Fatal("slio thread: exit code = 0, want a failure for an unregistered workspace")
	}
	got := errorLine(t, stderr)
	if !strings.Contains(got, "unknown.slack.com") || !strings.Contains(got, "myws") || !strings.Contains(got, "auth login") {
		t.Errorf("reported %q, want it to mention the host, registered profiles, and auth login", got)
	}
}

func TestRunThreadDownloadSavesAttachmentAndPrintsPath(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"ok":true,"messages":[{"type":"message","user":"U1","text":"hi","ts":"1234567890.000001","channel":"C1",`+
			`"files":[{"id":"F1","name":"report.txt","filetype":"text","size":13,"url_private":"%s/files-pri/T1-F1/report.txt"}]}],"has_more":false}`,
			srv.URL)
	})
	mux.HandleFunc("/files-pri/T1-F1/report.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "file contents")
	})

	got, stderr, code := runSlio(t, "thread", "https://myws.slack.com/archives/C1/p1234567890000001", "--download")
	if code != 0 {
		t.Fatalf("slio thread --download: exit code = %d, stderr = %s", code, stderr)
	}

	if !strings.Contains(got, "report.txt") {
		t.Errorf("output = %q, want it to mention report.txt", got)
	}
	if !strings.Contains(got, "saved to") {
		t.Errorf("output = %q, want it to show the local download path", got)
	}
}
