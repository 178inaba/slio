package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	f, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if f.DefaultProfile != "" {
		t.Errorf("DefaultProfile = %q, want empty", f.DefaultProfile)
	}
	if len(f.Profiles) != 0 {
		t.Errorf("Profiles = %v, want empty", f.Profiles)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	f := &File{
		DefaultProfile: "myws",
		Profiles: map[string]Profile{
			"myws": {Token: "xoxp-1", Host: "myws.slack.com", TeamID: "T1"},
		},
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	confDir := filepath.Join(dir, "slio")
	if info, err := os.Stat(confDir); err != nil {
		t.Fatalf("stat config dir: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm = %o, want 0700", perm)
	}

	path := filepath.Join(confDir, "config.json")
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("stat config file: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file perm = %o, want 0600", perm)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.DefaultProfile != "myws" {
		t.Errorf("DefaultProfile = %q, want myws", got.DefaultProfile)
	}
	p, ok := got.Profiles["myws"]
	if !ok {
		t.Fatalf("Profiles[myws] missing")
	}
	if p.Token != "xoxp-1" || p.Host != "myws.slack.com" || p.TeamID != "T1" {
		t.Errorf("Profiles[myws] = %+v, want {xoxp-1 myws.slack.com T1}", p)
	}
}

func fileWithProfiles(defaultProfile string, profiles map[string]Profile) *File {
	return &File{DefaultProfile: defaultProfile, Profiles: profiles}
}

func TestResolve(t *testing.T) {
	profiles := map[string]Profile{
		"myws":    {Token: "xoxp-myws", Host: "myws.slack.com", TeamID: "T1"},
		"otherws": {Token: "xoxp-other", Host: "otherws.slack.com", TeamID: "T2"},
	}

	tests := []struct {
		name        string
		file        *File
		flagProfile string
		urlHost     string
		env         map[string]string
		wantToken   string
		wantHost    string
		wantProfile string
		wantEnvTok  bool
		wantErr     bool
	}{
		{
			name:        "flag profile wins over SLIO_TOKEN",
			file:        fileWithProfiles("myws", profiles),
			flagProfile: "myws",
			env:         map[string]string{"SLIO_TOKEN": "xoxp-env"},
			wantToken:   "xoxp-myws",
			wantHost:    "myws.slack.com",
			wantProfile: "myws",
		},
		{
			name:        "flag profile not found",
			file:        fileWithProfiles("myws", profiles),
			flagProfile: "nope",
			wantErr:     true,
		},
		{
			name:        "flag profile conflicts with unregistered url host",
			file:        fileWithProfiles("myws", profiles),
			flagProfile: "myws",
			urlHost:     "unknown.slack.com",
			wantErr:     true,
		},
		{
			name:        "flag profile conflicts with a different registered url host",
			file:        fileWithProfiles("myws", profiles),
			flagProfile: "myws",
			urlHost:     "otherws.slack.com",
			wantErr:     true,
		},
		{
			name:        "flag profile matches url host",
			file:        fileWithProfiles("myws", profiles),
			flagProfile: "myws",
			urlHost:     "myws.slack.com",
			wantToken:   "xoxp-myws",
			wantHost:    "myws.slack.com",
			wantProfile: "myws",
		},
		{
			name:       "SLIO_TOKEN bypasses unregistered url host",
			file:       fileWithProfiles("myws", profiles),
			urlHost:    "unknown.slack.com",
			env:        map[string]string{"SLIO_TOKEN": "xoxp-env"},
			wantToken:  "xoxp-env",
			wantEnvTok: true,
		},
		{
			name:        "url host auto-selection",
			file:        fileWithProfiles("myws", profiles),
			urlHost:     "otherws.slack.com",
			wantToken:   "xoxp-other",
			wantHost:    "otherws.slack.com",
			wantProfile: "otherws",
		},
		{
			name:    "unregistered url host with no fallback",
			file:    fileWithProfiles("myws", profiles),
			urlHost: "unknown.slack.com",
			wantErr: true,
		},
		{
			name:        "SLIO_PROFILE selects profile",
			file:        fileWithProfiles("myws", profiles),
			env:         map[string]string{"SLIO_PROFILE": "otherws"},
			wantToken:   "xoxp-other",
			wantHost:    "otherws.slack.com",
			wantProfile: "otherws",
		},
		{
			name:    "SLIO_PROFILE not found",
			file:    fileWithProfiles("myws", profiles),
			env:     map[string]string{"SLIO_PROFILE": "nope"},
			wantErr: true,
		},
		{
			name:        "default profile used",
			file:        fileWithProfiles("myws", profiles),
			wantToken:   "xoxp-myws",
			wantHost:    "myws.slack.com",
			wantProfile: "myws",
		},
		{
			name:    "no profiles registered",
			file:    fileWithProfiles("", map[string]Profile{}),
			wantErr: true,
		},
		{
			name:        "SLACK_TOKEN is ignored entirely",
			file:        fileWithProfiles("myws", profiles),
			env:         map[string]string{"SLACK_TOKEN": "xoxb-should-be-ignored"},
			wantErr:     false,
			wantToken:   "xoxp-myws",
			wantHost:    "myws.slack.com",
			wantProfile: "myws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string { return tt.env[key] }

			creds, err := Resolve(tt.file, tt.flagProfile, tt.urlHost, getenv)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Resolve() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() error = %v, want nil", err)
			}
			if creds.Token != tt.wantToken {
				t.Errorf("Token = %q, want %q", creds.Token, tt.wantToken)
			}
			if creds.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", creds.Host, tt.wantHost)
			}
			if creds.Profile != tt.wantProfile {
				t.Errorf("Profile = %q, want %q", creds.Profile, tt.wantProfile)
			}
			if creds.ViaEnvToken != tt.wantEnvTok {
				t.Errorf("ViaEnvToken = %v, want %v", creds.ViaEnvToken, tt.wantEnvTok)
			}
		})
	}
}

// testdata/config-v1.json holds the literal bytes a pre-v2 build wrote,
// escapes and all. Reading it has to keep working across the encoder
// change, or an upgrade orphans the profiles someone already registered.
func TestLoadsFileWrittenByAPreV2Build(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	b, err := os.ReadFile(filepath.Join("testdata", "config-v1.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "slio"), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "slio", "config.json"), b, 0o600); err != nil {
		t.Fatalf("seed config file: %v", err)
	}

	f, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if f.DefaultProfile != "myws" {
		t.Errorf("DefaultProfile = %q, want myws", f.DefaultProfile)
	}
	// "team&co" is the interesting one: v1 escaped the ampersand, and it
	// has to come back as a profile name that still resolves.
	for name, want := range map[string]Profile{
		"myws":    {Token: "xoxp-1", Host: "myws.slack.com", TeamID: "T1"},
		"team&co": {Token: "xoxp-2", Host: "team.slack.com", TeamID: "T2"},
	} {
		if got := f.Profiles[name]; got != want {
			t.Errorf("Profiles[%q] = %+v, want %+v", name, got, want)
		}
	}
}
