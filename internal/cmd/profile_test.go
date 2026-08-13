package cmd

import (
	"strings"
	"testing"

	"github.com/178inaba/slio/internal/config"
)

func TestProfileListNoProfiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stdout, stderr, err := runSlio(t, "profile", "list")
	if err != nil {
		t.Fatalf("slio profile list: %v", err)
	}
	if !strings.Contains(stderr, "No profiles registered") {
		t.Errorf("stderr = %q, want mention of no profiles registered", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty when no profile is registered", stdout)
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

	got, _, err := runSlio(t, "profile", "list")
	if err != nil {
		t.Fatalf("slio profile list: %v", err)
	}

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

	stdout, stderr, err := runSlio(t, "profile", "use", "otherws")
	if err != nil {
		t.Fatalf("slio profile use: %v", err)
	}
	if !strings.Contains(stderr, "otherws") {
		t.Errorf("stderr = %q, want mention of otherws", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
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

	_, _, err := runSlio(t, "profile", "use", "nope")
	if err == nil {
		t.Fatal("slio profile use: error = nil, want error")
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
