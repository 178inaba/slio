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

// serveThread stands up a Slack API stub serving a two-message thread: a
// parent ("1234567890.000001") and one reply ("1234567890.000002").
func serveThread(t *testing.T) {
	t.Helper()
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"conversations.replies": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
				`{"type":"message","user":"U1","text":"parent message","ts":"1234567890.000001","channel":"C1","reply_count":1},`+
				`{"type":"message","user":"U1","text":"a reply","ts":"1234567890.000002","channel":"C1"}`+
				`],"has_more":false,"response_metadata":{"next_cursor":""}}`)
		},
		"users.info": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","profile":{"display_name":"Alice"}}}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)
}

// markedBlocks returns the rendered message blocks carrying the
// linked-message marker, so a test can assert which message was marked
// without pinning the marker's exact position. It keys off the marker glyph
// rather than its wording, which the not-found notice also uses.
func markedBlocks(out string) []string {
	var marked []string
	for _, block := range strings.Split(out, "\n---\n") {
		if strings.Contains(block, "🎯") {
			marked = append(marked, block)
		}
	}
	return marked
}

func TestRunThreadRendersFullThread(t *testing.T) {
	serveThread(t)

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

	mux.HandleFunc("/conversations.replies", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"ok":true,"messages":[{"type":"message","user":"U1","text":"hi","ts":"1234567890.000001","channel":"C1",`+
			`"files":[{"id":"F1","name":"report.txt","filetype":"text","size":13,"url_private":"%s/files-pri/T1-F1/report.txt"}]}],"has_more":false}`,
			srv.URL)
	})
	mux.HandleFunc("/files-pri/T1-F1/report.txt", func(w http.ResponseWriter, _ *http.Request) {
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

func TestRunThreadMarksTheReplyAReplyPermalinkPointsAt(t *testing.T) {
	serveThread(t)

	got, stderr, code := runSlio(t, "thread",
		"https://myws.slack.com/archives/C1/p1234567890000002?thread_ts=1234567890.000001&cid=C1")
	if code != 0 {
		t.Fatalf("slio thread: exit code = %d, stderr = %s", code, stderr)
	}

	if marked := markedBlocks(got); len(marked) != 1 || !strings.Contains(marked[0], "a reply") {
		t.Errorf("marked blocks = %q, want only the reply the URL points at; output = %q", marked, got)
	}
}

func TestRunThreadMarksTheParentAParentPermalinkPointsAt(t *testing.T) {
	serveThread(t)

	got, stderr, code := runSlio(t, "thread", "https://myws.slack.com/archives/C1/p1234567890000001")
	if code != 0 {
		t.Fatalf("slio thread: exit code = %d, stderr = %s", code, stderr)
	}

	if marked := markedBlocks(got); len(marked) != 1 || !strings.Contains(marked[0], "parent message") {
		t.Errorf("marked blocks = %q, want only the parent the URL points at; output = %q", marked, got)
	}
}

func TestRunThreadJSONMarksOnlyTheLinkedMessage(t *testing.T) {
	serveThread(t)

	got, stderr, code := runSlio(t, "thread",
		"https://myws.slack.com/archives/C1/p1234567890000002?thread_ts=1234567890.000001&cid=C1",
		"--format", "json")
	if code != 0 {
		t.Fatalf("slio thread --format json: exit code = %d, stderr = %s", code, stderr)
	}

	decoded := decodeMessagesEnvelope(t, got)
	if len(decoded.Messages) != 2 {
		t.Fatalf("messages length = %d, want 2; output = %s", len(decoded.Messages), got)
	}
	if decoded.Messages[1]["linked"] != true {
		t.Errorf("reply linked = %v, want true", decoded.Messages[1]["linked"])
	}
	if _, ok := decoded.Messages[0]["linked"]; ok {
		t.Errorf("parent linked key present = %v, want omitted", decoded.Messages[0]["linked"])
	}
	if decoded.Notice != "" {
		t.Errorf("notice = %q, want none when the target was found", decoded.Notice)
	}
}

func TestRunThreadMissingTargetNoticesAndSucceeds(t *testing.T) {
	serveThread(t)

	// A ts no message in the thread carries — a deleted reply, or a
	// hand-edited URL.
	got, stderr, code := runSlio(t, "thread",
		"https://myws.slack.com/archives/C1/p1234567890999999?thread_ts=1234567890.000001&cid=C1")
	if code != 0 {
		t.Fatalf("slio thread: exit code = %d, want 0 — the thread fetch itself succeeded; stderr = %s", code, stderr)
	}

	if !strings.Contains(got, "1234567890.999999") {
		t.Errorf("output = %q, want a notice naming the ts that was not found", got)
	}
	if marked := markedBlocks(got); len(marked) != 0 {
		t.Errorf("marked blocks = %q, want none when the target is missing", marked)
	}
	for _, want := range []string{"parent message", "a reply"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to still contain %q", got, want)
		}
	}
}

func TestRunThreadMissingTargetNoticeReachesJSONEnvelope(t *testing.T) {
	serveThread(t)

	got, stderr, code := runSlio(t, "thread",
		"https://myws.slack.com/archives/C1/p1234567890999999?thread_ts=1234567890.000001&cid=C1",
		"--format", "json")
	if code != 0 {
		t.Fatalf("slio thread --format json: exit code = %d, stderr = %s", code, stderr)
	}

	decoded := decodeMessagesEnvelope(t, got)
	if !strings.Contains(decoded.Notice, "1234567890.999999") {
		t.Errorf("notice = %q, want it to name the ts that was not found", decoded.Notice)
	}
	for i, m := range decoded.Messages {
		if _, ok := m["linked"]; ok {
			t.Errorf("messages[%d] linked key present = %v, want omitted when the target is missing", i, m["linked"])
		}
	}
}
