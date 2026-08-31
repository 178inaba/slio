package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/format"
	"github.com/178inaba/slio/internal/slackclient"
	"github.com/spf13/cobra"
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
	slackClientFactory = func(_ string) *slackclient.Client {
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
		"auth.test": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"url":"https://myws.slack.com/","team_id":"T1"}`)
		},
		"conversations.history": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","text":"hi","ts":"1.000001",`+
				`"channel":"C1","reply_count":2}],"has_more":false}`)
		},
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	got, stderr, code := runSlio(t, "history", "C1")
	if code != 0 {
		t.Fatalf("slio history: exit code = %d, stderr = %s", code, stderr)
	}

	if !strings.Contains(got, "myws.slack.com") {
		t.Errorf("output = %q, want a thread permalink built from the auth.test-resolved host", got)
	}
}

// A format.Format converted from an arbitrary string bypasses Set, so
// writeMessages keeps its own guard rather than trusting the type — and
// falls back to neither renderer. Only a direct call can reach it: the flag
// itself can no longer carry an unknown value.
func TestWriteMessagesUnknownFormat(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := writeMessages(cmd, format.Format("yaml"), nil, func(string) string { return "" }, "", "")
	if err == nil || !strings.Contains(err.Error(), "yaml") {
		t.Fatalf("writeMessages(yaml) error = %v, want an unsupported-format error naming it", err)
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want nothing written for an unsupported format", out.String())
	}
}
