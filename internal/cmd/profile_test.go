package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/config"
	"github.com/spf13/cobra"
)

// newTestRoot returns a freshly built command tree wired to separate stdout
// and stderr buffers, so tests can assert which stream a message went to.
// Building the tree per test keeps flag values from leaking between cases.
func newTestRoot(t *testing.T) (root *cobra.Command, stdout, stderr *bytes.Buffer) {
	t.Helper()
	testRoot := newRootCmd()
	var out, errOut bytes.Buffer
	testRoot.SetOut(&out)
	testRoot.SetErr(&errOut)
	return testRoot, &out, &errOut
}

func TestProfileListNoProfiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	root, out, errOut := newTestRoot(t)
	root.SetArgs([]string{"profile", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(errOut.String(), "No profiles registered") {
		t.Errorf("stderr = %q, want mention of no profiles registered", errOut.String())
	}
	if out.Len() > 0 {
		t.Errorf("stdout = %q, want empty when no profile is registered", out.String())
	}
}

func TestProfileListShowsProfiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws":    {Token: "xoxp-1", Host: "myws.slack.com"},
			"otherws": {Token: "xoxp-2", Host: "otherws.slack.com"},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	root, out, _ := newTestRoot(t)
	root.SetArgs([]string{"profile", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "myws") || !strings.Contains(got, "myws.slack.com") {
		t.Errorf("output = %q, want mention of myws profile", got)
	}
	if !strings.Contains(got, "otherws") || !strings.Contains(got, "otherws.slack.com") {
		t.Errorf("output = %q, want mention of otherws profile", got)
	}
	if !strings.Contains(got, "default") {
		t.Errorf("output = %q, want a marker for the default profile", got)
	}
}

func TestProfileUseSwitchesDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws":    {Token: "xoxp-1", Host: "myws.slack.com"},
			"otherws": {Token: "xoxp-2", Host: "otherws.slack.com"},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	root, out, errOut := newTestRoot(t)
	root.SetArgs([]string{"profile", "use", "otherws"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(errOut.String(), "otherws") {
		t.Errorf("stderr = %q, want mention of otherws", errOut.String())
	}
	if out.Len() > 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}

	got, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if got.DefaultProfile != "otherws" {
		t.Errorf("DefaultProfile = %q, want otherws", got.DefaultProfile)
	}
}

func TestProfileUseUnknownName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := &config.File{
		DefaultProfile: "myws",
		Profiles: map[string]config.Profile{
			"myws": {Token: "xoxp-1", Host: "myws.slack.com"},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatalf("seed config Save() error = %v", err)
	}

	root, _, _ := newTestRoot(t)
	root.SetArgs([]string{"profile", "use", "nope"})
	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "myws") {
		t.Errorf("error = %v, want mention of registered profiles", err)
	}

	got, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if got.DefaultProfile != "myws" {
		t.Errorf("DefaultProfile = %q, want unchanged myws", got.DefaultProfile)
	}
}
