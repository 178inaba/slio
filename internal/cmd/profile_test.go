package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/config"
	"github.com/spf13/cobra"
)

// newTestCmd returns a command wired to separate stdout and stderr buffers,
// so tests can assert which stream a message went to.
func newTestCmd(t *testing.T) (cmd *cobra.Command, stdout, stderr *bytes.Buffer) {
	t.Helper()
	testCmd := &cobra.Command{}
	var out, errOut bytes.Buffer
	testCmd.SetOut(&out)
	testCmd.SetErr(&errOut)
	return testCmd, &out, &errOut
}

func TestProfileListNoProfiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	testCmd, out, errOut := newTestCmd(t)
	if err := runProfileList(testCmd, nil); err != nil {
		t.Fatalf("runProfileList() error = %v", err)
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

	testCmd, out, _ := newTestCmd(t)
	if err := runProfileList(testCmd, nil); err != nil {
		t.Fatalf("runProfileList() error = %v", err)
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

	testCmd, out, errOut := newTestCmd(t)
	if err := runProfileUse(testCmd, []string{"otherws"}); err != nil {
		t.Fatalf("runProfileUse() error = %v", err)
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

	testCmd, _, _ := newTestCmd(t)
	err := runProfileUse(testCmd, []string{"nope"})
	if err == nil {
		t.Fatal("runProfileUse() error = nil, want error")
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
