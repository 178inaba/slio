package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/slackclient"
)

func TestResolveWorkspaceViaSlioToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SLIO_TOKEN", "xoxp-env-token")

	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	creds, host, cacheKey, err := resolveWorkspace(context.Background(), "", "")
	if err != nil {
		t.Fatalf("resolveWorkspace() error = %v", err)
	}
	if creds.Token != "xoxp-env-token" || !creds.ViaEnvToken {
		t.Errorf("creds = %+v, want Token=xoxp-env-token ViaEnvToken=true", creds)
	}
	if host != "myws.slack.com" {
		t.Errorf("host = %q, want myws.slack.com (resolved via auth.test)", host)
	}
	if cacheKey != "team-T1" {
		t.Errorf("cacheKey = %q, want team-T1", cacheKey)
	}
}

func TestResolveWorkspaceViaProfileDoesNotCallAuthTest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	seedProfile(t, "myws.slack.com")

	orig := slackClientFactory
	t.Cleanup(func() { slackClientFactory = orig })
	slackClientFactory = func(token string) *slackclient.Client {
		t.Fatal("slackClientFactory should not be called when a profile resolves the credentials")
		return nil
	}

	_, host, cacheKey, err := resolveWorkspace(context.Background(), "", "")
	if err != nil {
		t.Fatalf("resolveWorkspace() error = %v", err)
	}
	if host != "myws.slack.com" || cacheKey != "myws" {
		t.Errorf("host/cacheKey = %q/%q, want myws.slack.com/myws", host, cacheKey)
	}
}

func TestRunHistoryViaSlioTokenIncludesThreadPermalink(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SLIO_TOKEN", "xoxp-env-token")

	srv := httptest.NewServer(newSlackAPIMux(map[string]http.HandlerFunc{
		"auth.test": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"url":"https://myws.slack.com/","team_id":"T1"}`)
		},
		"conversations.history": func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"hi","ts":"1.000001",`+
				`"channel":"C1","reply_count":2}],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, _, err := runSlio(t, "history", "C1")
	if err != nil {
		t.Fatalf("slio history: %v", err)
	}

	if !strings.Contains(got, "myws.slack.com") {
		t.Errorf("output = %q, want a thread permalink built from the auth.test-resolved host", got)
	}
}
