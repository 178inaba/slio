package cmd

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// updateGolden rewrites testdata/ instead of comparing against it:
//
//	go test ./internal/cmd/ -run TestGolden -update
//
// What slio writes to stdout is a contract its callers branch on
// mechanically, and every other test here decodes the JSON before
// asserting, which hides any change in how a byte is escaped or a key
// ordered. These files are the byte-level record of that contract, so a
// change to it has to show up as a reviewable diff.
var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata/")

// U+2028 and U+2029 are built from their code points rather than written
// literally: they are invisible in source, and a fixture has to carry them
// because they are escaped on the way out.
var (
	lineSeparator      = string(rune(0x2028))
	paragraphSeparator = string(rune(0x2029))
)

// entityText exercises the characters that JSON encoders disagree about.
// Slack sends < > & as entities and slio renders them back to the bare
// characters, so they reach the encoder no matter which format is asked
// for.
var entityText = `a &lt;b&gt; &amp; c` + lineSeparator + `then` + paragraphSeparator + `end`

// pinTimeZone makes rendered timestamps reproducible. Both formats carry
// the local zone -- Markdown through formatLocalTime, JSON through the
// offset time.Unix puts on the value -- so without this the golden files
// would only match on the machine that wrote them.
func pinTimeZone(t *testing.T) {
	t.Helper()
	orig := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = orig })
}

// assertGolden compares got against testdata/<name>, or rewrites that file
// when -update is set.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("create testdata directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v (run `go test ./internal/cmd/ -run TestGolden -update` to write it)", path, err)
	}
	if got != string(want) {
		t.Errorf("output does not match %s\n got: %q\nwant: %q", path, got, want)
	}
}

// forEachFormat runs fn once per output format, naming the subtest after
// the format so a failure says which one drifted.
func forEachFormat(t *testing.T, fn func(t *testing.T, outputFormat string)) {
	t.Helper()
	for _, outputFormat := range []string{"md", "json"} {
		t.Run(outputFormat, func(t *testing.T) {
			pinTimeZone(t)
			seedProfile(t, "myws.slack.com")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			fn(t, outputFormat)
		})
	}
}

func TestGoldenThread(t *testing.T) {
	forEachFormat(t, func(t *testing.T, outputFormat string) {
		srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
			"conversations.replies": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
					`{"type":"message","user":"U1","text":"parent `+entityText+` for <@U1>","ts":"1234567890.000001","channel":"C1",`+
					`"reply_count":1,`+
					`"reactions":[{"name":"tada","count":2},{"name":"eyes","count":1}],`+
					`"files":[{"id":"F1","name":"report &amp; notes.txt","filetype":"text","size":13}]},`+
					`{"type":"message","user":"U1","text":"a reply","ts":"1234567890.000002","channel":"C1",`+
					`"edited":{"user":"U1","ts":"1234567890.000003"},`+
					`"attachments":[{"text":"quoted &lt;i&gt; block"}]}`+
					`],"has_more":false,"response_metadata":{"next_cursor":""}}`)
			},
			"users.info": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","profile":{"display_name":"Alice"}}}`)
			},
		}))
		t.Cleanup(srv.Close)
		stubSlackClientFactory(t, srv)

		// The permalink points at the reply, so the reply carries the
		// linked-message marker and the parent does not.
		got, stderr, code := runSlio(t, "thread",
			"https://myws.slack.com/archives/C1/p1234567890000002",
			"--format", outputFormat)
		if code != 0 {
			t.Fatalf("slio thread --format %s: exit code = %d, stderr = %s", outputFormat, code, stderr)
		}
		assertGolden(t, "thread."+outputFormat, got)
	})
}

func TestGoldenHistory(t *testing.T) {
	forEachFormat(t, func(t *testing.T, outputFormat string) {
		srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
			// Three messages against --limit 2, so the output leads with a
			// truncation notice. The first also carries reply_count, which
			// is what puts a thread permalink (and its & separators) in the
			// output.
			"conversations.history": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"ok":true,"messages":[`+
					`{"type":"message","user":"U1","text":"newest `+entityText+`","ts":"1234567890.000003","channel":"C1","reply_count":4},`+
					`{"type":"message","user":"U1","text":"middle","ts":"1234567890.000002","channel":"C1"},`+
					`{"type":"message","subtype":"channel_join","user":"U1","text":"Alice has joined the channel","ts":"1234567890.000001","channel":"C1"}`+
					`],"has_more":false}`)
			},
			"users.info": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"ok":true,"user":{"id":"U1","profile":{"display_name":"Alice"}}}`)
			},
		}))
		t.Cleanup(srv.Close)
		stubSlackClientFactory(t, srv)

		got, stderr, code := runSlio(t, "history", "C1", "--limit", "2", "--format", outputFormat)
		if code != 0 {
			t.Fatalf("slio history --format %s: exit code = %d, stderr = %s", outputFormat, code, stderr)
		}
		assertGolden(t, "history."+outputFormat, got)
	})
}

func TestGoldenSearch(t *testing.T) {
	forEachFormat(t, func(t *testing.T, outputFormat string) {
		// total 5 against --limit 1, so the output ends with a
		// more-results notice.
		srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
			"search.messages": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"ok":true,"messages":{"matches":[`+
					`{"username":"Bob","text":"match `+entityText+`","ts":"1234567890.000001",`+
					`"permalink":"https://myws.slack.com/archives/C1/p1234567890000001?a=1&amp;b=2",`+
					`"attachments":[{"text":"quoted match"}]}`+
					`],"total":5,"pagination":{"page_count":1}}}`)
			},
		}))
		t.Cleanup(srv.Close)
		stubSlackClientFactory(t, srv)

		got, stderr, code := runSlio(t, "search", "hello", "--limit", "1", "--format", outputFormat)
		if code != 0 {
			t.Fatalf("slio search --format %s: exit code = %d, stderr = %s", outputFormat, code, stderr)
		}
		assertGolden(t, "search."+outputFormat, got)
	})
}

func TestGoldenChannelList(t *testing.T) {
	forEachFormat(t, func(t *testing.T, outputFormat string) {
		srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
			"users.conversations": func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"ok":true,"channels":[`+
					`{"id":"C1","name":"general"},`+
					`{"id":"C2","name":"random"}`+
					`],"response_metadata":{"next_cursor":""}}`)
			},
		}))
		t.Cleanup(srv.Close)
		stubSlackClientFactory(t, srv)

		got, stderr, code := runSlio(t, "channel", "list", "--format", outputFormat)
		if code != 0 {
			t.Fatalf("slio channel list --format %s: exit code = %d, stderr = %s", outputFormat, code, stderr)
		}
		assertGolden(t, "channel-list."+outputFormat, got)
	})
}
