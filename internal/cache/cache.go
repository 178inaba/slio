// Package cache manages slio's on-disk channel and user lookup caches. It
// exists to avoid repeated conversations.list/users.info calls against the
// 90s deadline; entries older than TTL are treated as a miss, and callers
// are expected to refresh from the API and write the fresh result back.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TTL is how long a cached lookup stays valid before it's treated as a
// miss and must be refreshed from the API.
const TTL = 24 * time.Hour

// Store manages the channel/user caches for one cache key: a profile name,
// or (when SLIO_TOKEN bypasses profile resolution) a workspace team ID.
type Store struct {
	dir string
}

// Dir returns the cache directory for a given key, honoring
// XDG_CACHE_HOME.
func Dir(key string) (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "slio", key), nil
}

// Open returns a Store for the given cache key.
func Open(key string) (*Store, error) {
	dir, err := Dir(key)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

type channelEntry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	FetchedAt time.Time `json:"fetched_at"`
}

type channelsFile struct {
	Channels []channelEntry `json:"channels"`
}

// ChannelInfo is a single channel to cache, keyed by name for #name
// resolution.
type ChannelInfo struct {
	ID   string
	Name string
}

// ChannelIDByName returns the cached ID for a channel name, and whether the
// entry was found and still fresh as of now. A false result (miss or
// stale) means the caller should refresh from the API and call
// PutChannels.
func (s *Store) ChannelIDByName(name string, now time.Time) (id string, ok bool, err error) {
	var f channelsFile
	found, err := s.readJSON("channels.json", &f)
	if err != nil || !found {
		return "", false, err
	}
	for _, c := range f.Channels {
		if c.Name != name {
			continue
		}
		if now.Sub(c.FetchedAt) > TTL {
			return "", false, nil
		}
		return c.ID, true, nil
	}
	return "", false, nil
}

// PutChannels replaces the cached channel listing. Channel names/IDs are
// only ever available as a full list from users.conversations, so a
// partial merge doesn't apply here.
func (s *Store) PutChannels(channels []ChannelInfo, now time.Time) error {
	f := channelsFile{Channels: make([]channelEntry, len(channels))}
	for i, c := range channels {
		f.Channels[i] = channelEntry{ID: c.ID, Name: c.Name, FetchedAt: now}
	}
	return s.writeJSON("channels.json", f)
}

type userEntry struct {
	DisplayName string    `json:"display_name"`
	FetchedAt   time.Time `json:"fetched_at"`
}

type usersFile struct {
	Users map[string]userEntry `json:"users"`
}

// UserDisplayName returns the cached display name for a user ID, and
// whether the entry was found and still fresh as of now.
func (s *Store) UserDisplayName(userID string, now time.Time) (name string, ok bool, err error) {
	var f usersFile
	found, err := s.readJSON("users.json", &f)
	if err != nil || !found {
		return "", false, err
	}
	e, exists := f.Users[userID]
	if !exists || now.Sub(e.FetchedAt) > TTL {
		return "", false, nil
	}
	return e.DisplayName, true, nil
}

// PutUser upserts a single user's cached display name, leaving other
// cached users untouched (unlike channels, users are looked up and
// refreshed one at a time via users.info).
func (s *Store) PutUser(userID, displayName string, now time.Time) error {
	var f usersFile
	if _, err := s.readJSON("users.json", &f); err != nil {
		return err
	}
	if f.Users == nil {
		f.Users = map[string]userEntry{}
	}
	f.Users[userID] = userEntry{DisplayName: displayName, FetchedAt: now}
	return s.writeJSON("users.json", f)
}

func (s *Store) readJSON(name string, v any) (found bool, err error) {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read cache file %s: %w", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parse cache file %s: %w", name, err)
	}
	return true, nil
}

func (s *Store) writeJSON(name string, v any) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache file %s: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, name), data, 0o644); err != nil {
		return fmt.Errorf("write cache file %s: %w", name, err)
	}
	return nil
}
