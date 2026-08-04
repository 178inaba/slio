// Package config manages slio's on-disk configuration (registered
// workspace profiles and their tokens) and resolves which credentials an
// invocation should use.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Profile holds the stored credentials for a single registered workspace.
type Profile struct {
	Token  string `json:"token"`
	Host   string `json:"host"`
	TeamID string `json:"team_id"`
}

// File is the on-disk representation of ~/.config/slio/config.json.
type File struct {
	DefaultProfile string             `json:"default_profile"`
	Profiles       map[string]Profile `json:"profiles"`
}

// Dir returns the directory slio's config file lives in, honoring
// XDG_CONFIG_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "slio"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "slio"), nil
}

// Path returns the full path to the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file. A missing file is not an error: it returns an
// empty File, since that's the normal state before the first `auth login`.
func Load() (*File, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}
	if f.Profiles == nil {
		f.Profiles = map[string]Profile{}
	}
	return &f, nil
}

// Save writes the config file, creating its directory if needed. The
// directory and file permissions are restricted (0700/0600) since the file
// holds plaintext tokens.
func (f *File) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

// Credentials is the resolved outcome of Resolve: the token to use for a
// single invocation, plus the profile metadata behind it when one was
// resolved.
type Credentials struct {
	Token string
	// Host is the profile's stored workspace host. Empty when the token
	// came from SLIO_TOKEN, since no profile was resolved in that case.
	Host string
	// Profile is the resolved profile name. Empty when the token came from
	// SLIO_TOKEN.
	Profile string
	// ViaEnvToken is true when SLIO_TOKEN supplied the token, bypassing
	// profile resolution entirely.
	ViaEnvToken bool
}

// Resolve implements slio's credential precedence for a single invocation:
//
//  1. --profile flag (flagProfile): uses that profile's stored token,
//     ignoring SLIO_TOKEN. Errors if the profile doesn't exist, or if
//     urlHost is set and doesn't match the profile's host (calling the
//     wrong workspace would yield an indistinguishable channel_not_found).
//  2. SLIO_TOKEN: bypasses profile resolution and the unregistered-host
//     check entirely — the caller takes responsibility for the token.
//  3. urlHost auto-selection: the profile whose host matches urlHost.
//     Errors if no profile is registered for that host (no fallback to the
//     default profile, for the same channel_not_found reason as above).
//  4. SLIO_PROFILE.
//  5. The default profile.
//
// SLACK_TOKEN is deliberately never read here.
func Resolve(f *File, flagProfile, urlHost string, getenv func(string) string) (Credentials, error) {
	if flagProfile != "" {
		p, ok := f.Profiles[flagProfile]
		if !ok {
			return Credentials{}, fmt.Errorf("profile %q not found; registered profiles: %s",
				flagProfile, registeredProfilesList(f))
		}
		if urlHost != "" && urlHost != p.Host {
			return Credentials{}, fmt.Errorf(
				"--profile %q (host %s) conflicts with the URL's host %s; pass a matching --profile or omit it",
				flagProfile, p.Host, urlHost)
		}
		return Credentials{Token: p.Token, Host: p.Host, Profile: flagProfile}, nil
	}

	if token := getenv("SLIO_TOKEN"); token != "" {
		return Credentials{Token: token, ViaEnvToken: true}, nil
	}

	if urlHost != "" {
		for name, p := range f.Profiles {
			if p.Host == urlHost {
				return Credentials{Token: p.Token, Host: p.Host, Profile: name}, nil
			}
		}
		return Credentials{}, fmt.Errorf(
			"no profile registered for %s; registered profiles: %s; run `slio auth login` to register it",
			urlHost, registeredProfilesList(f))
	}

	if name := getenv("SLIO_PROFILE"); name != "" {
		p, ok := f.Profiles[name]
		if !ok {
			return Credentials{}, fmt.Errorf("SLIO_PROFILE %q not found; registered profiles: %s",
				name, registeredProfilesList(f))
		}
		return Credentials{Token: p.Token, Host: p.Host, Profile: name}, nil
	}

	if f.DefaultProfile == "" {
		return Credentials{}, fmt.Errorf("no profiles registered; run `slio auth login` first")
	}
	p, ok := f.Profiles[f.DefaultProfile]
	if !ok {
		return Credentials{}, fmt.Errorf("default profile %q not found; registered profiles: %s",
			f.DefaultProfile, registeredProfilesList(f))
	}
	return Credentials{Token: p.Token, Host: p.Host, Profile: f.DefaultProfile}, nil
}

func registeredProfilesList(f *File) string {
	if len(f.Profiles) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(f.Profiles))
	for name := range f.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
