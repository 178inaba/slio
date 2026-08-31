package cmd

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/178inaba/slio/internal/cache"
)

func TestRunChannelListMarkdown(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"},{"id":"C2","name":"random"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, stderr, code := runSlio(t, "channel", "list")
	if code != 0 {
		t.Fatalf("slio channel list: exit code = %d, stderr = %s", code, stderr)
	}

	if !strings.Contains(got, "general") || !strings.Contains(got, "random") {
		t.Errorf("output = %q, want both channels listed", got)
	}
}

func TestRunChannelListJSON(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, stderr, code := runSlio(t, "channel", "list", "--format", "json")
	if code != 0 {
		t.Fatalf("slio channel list --format json: exit code = %d, stderr = %s", code, stderr)
	}

	// Decoded generically rather than into format.Channel: decoding into
	// the struct that declares the tags would restate the key names rather
	// than check them, and would keep passing through a rename.
	var channels []map[string]any
	if err := json.Unmarshal([]byte(got), &channels); err != nil {
		t.Fatalf("unmarshal output: %v; output = %s", err, got)
	}
	if len(channels) != 1 {
		t.Fatalf("channels = %v, want one entry", channels)
	}
	if channels[0]["id"] != "C1" || channels[0]["name"] != "general" {
		t.Errorf("channels[0] = %v, want id C1 and name general", channels[0])
	}
}

func TestRunChannelListPopulatesCacheForNameResolution(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	if _, stderr, code := runSlio(t, "channel", "list"); code != 0 {
		t.Fatalf("slio channel list: exit code = %d, stderr = %s", code, stderr)
	}

	store, err := cache.Open("myws")
	if err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	id, ok, err := store.ChannelIDByName("general", time.Now())
	if err != nil {
		t.Fatalf("ChannelIDByName() error = %v", err)
	}
	if !ok || id != "C1" {
		t.Errorf("ChannelIDByName(general) = (%q, %v), want (C1, true)", id, ok)
	}
}
