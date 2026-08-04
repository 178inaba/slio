package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/178inaba/slio/internal/cache"
	"github.com/spf13/cobra"
)

func TestRunChannelListMarkdown(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"},{"id":"C2","name":"random"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	testCmd := &cobra.Command{}
	var out bytes.Buffer
	testCmd.SetOut(&out)

	if err := runChannelList(testCmd, nil); err != nil {
		t.Fatalf("runChannelList() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "general") || !strings.Contains(got, "random") {
		t.Errorf("output = %q, want both channels listed", got)
	}
}

func TestRunChannelListJSON(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	formatFlag = "json"
	t.Cleanup(func() { formatFlag = "md" })

	testCmd := &cobra.Command{}
	var out bytes.Buffer
	testCmd.SetOut(&out)

	if err := runChannelList(testCmd, nil); err != nil {
		t.Fatalf("runChannelList() error = %v", err)
	}

	var channels []jsonChannel
	if err := json.Unmarshal(out.Bytes(), &channels); err != nil {
		t.Fatalf("unmarshal output: %v; output = %s", err, out.String())
	}
	if len(channels) != 1 || channels[0].ID != "C1" || channels[0].Name != "general" {
		t.Errorf("channels = %+v, want [{C1 general}]", channels)
	}
}

func TestRunChannelListPopulatesCacheForNameResolution(t *testing.T) {
	seedProfile(t, "myws.slack.com")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"users.conversations": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}]}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	testCmd := &cobra.Command{}
	testCmd.SetOut(&bytes.Buffer{})
	if err := runChannelList(testCmd, nil); err != nil {
		t.Fatalf("runChannelList() error = %v", err)
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
