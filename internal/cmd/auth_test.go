package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/config"
	"github.com/178inaba/slio/internal/slackclient"
	"github.com/spf13/cobra"
)

func newAuthTestServer(t *testing.T, host, teamID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"ok":true,"url":"https://%s/","team_id":"%s"}`, host, teamID)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stubSlackClientFactory(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := slackClientFactory
	slackClientFactory = func(token string) *slackclient.Client {
		return slackclient.New(token, slackclient.WithAPIURL(srv.URL+"/"))
	}
	t.Cleanup(func() { slackClientFactory = orig })
}

func runAuthLoginForTest(t *testing.T, stdin string) (string, error) {
	t.Helper()
	testCmd := &cobra.Command{}
	testCmd.SetIn(strings.NewReader(stdin))
	var out bytes.Buffer
	testCmd.SetOut(&out)

	err := runAuthLogin(testCmd, nil)
	return out.String(), err
}

func TestAuthLoginRegistersNewProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	out, err := runAuthLoginForTest(t, "xoxp-abc\n\n")
	if err != nil {
		t.Fatalf("runAuthLogin() error = %v, output = %s", err, out)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if f.DefaultProfile != "myws" {
		t.Errorf("DefaultProfile = %q, want myws", f.DefaultProfile)
	}
	p, ok := f.Profiles["myws"]
	if !ok {
		t.Fatalf("profile %q not registered", "myws")
	}
	if p.Token != "xoxp-abc" || p.Host != "myws.slack.com" || p.TeamID != "T1" {
		t.Errorf("profile = %+v, want {xoxp-abc myws.slack.com T1}", p)
	}
}

func TestAuthLoginCustomProfileName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	if _, err := runAuthLoginForTest(t, "xoxp-abc\ncustomname\n"); err != nil {
		t.Fatalf("runAuthLogin() error = %v", err)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if _, ok := f.Profiles["customname"]; !ok {
		t.Fatalf("profile %q not registered; profiles = %+v", "customname", f.Profiles)
	}
}

func TestAuthLoginRejectsNonUserToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// No server stub: a network call here would fail the test by connecting
	// to a closed/invalid address, proving the prefix check short-circuits
	// before any API call.
	slackClientFactory = func(token string) *slackclient.Client {
		t.Fatal("slackClientFactory should not be called for a rejected token")
		return nil
	}
	t.Cleanup(func() { slackClientFactory = defaultSlackClientFactory })

	_, err := runAuthLoginForTest(t, "xoxb-bot-token\n")
	if err == nil {
		t.Fatal("runAuthLogin() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "xoxp-") {
		t.Errorf("error = %v, want mention of xoxp- prefix", err)
	}

	if _, statErr := config.Load(); statErr == nil {
		f, _ := config.Load()
		if len(f.Profiles) != 0 {
			t.Errorf("profiles = %+v, want none saved", f.Profiles)
		}
	}
}

func TestAuthLoginAuthTestFailureDoesNotSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":false,"error":"invalid_auth"}`)
	}))
	t.Cleanup(srv.Close)
	stubSlackClientFactory(t, srv)

	_, err := runAuthLoginForTest(t, "xoxp-bad\n")
	if err == nil {
		t.Fatal("runAuthLogin() error = nil, want error")
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("profiles = %+v, want none saved", f.Profiles)
	}
}

func TestAuthLoginReRegisterSameWorkspaceConfirmed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	existing := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws": {Token: "xoxp-old", Host: "myws.slack.com", TeamID: "T1"},
		},
	}
	if err := existing.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	out, err := runAuthLoginForTest(t, "xoxp-new\ny\n")
	if err != nil {
		t.Fatalf("runAuthLogin() error = %v, output = %s", err, out)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	p := f.Profiles["myws"]
	if p.Token != "xoxp-new" {
		t.Errorf("Token = %q, want xoxp-new (overwritten)", p.Token)
	}
	if len(f.Profiles) != 1 {
		t.Errorf("Profiles = %+v, want exactly one entry (myws)", f.Profiles)
	}
}

func TestAuthLoginReRegisterSameWorkspaceDeclined(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newAuthTestServer(t, "myws.slack.com", "T1")
	stubSlackClientFactory(t, srv)

	existing := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws": {Token: "xoxp-old", Host: "myws.slack.com", TeamID: "T1"},
		},
	}
	if err := existing.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	out, err := runAuthLoginForTest(t, "xoxp-new\nn\n")
	if err != nil {
		t.Fatalf("runAuthLogin() error = %v, output = %s", err, out)
	}
	if !strings.Contains(out, "Aborted") {
		t.Errorf("output = %q, want mention of Aborted", out)
	}

	f, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if f.Profiles["myws"].Token != "xoxp-old" {
		t.Errorf("Token = %q, want xoxp-old (unchanged)", f.Profiles["myws"].Token)
	}
}
