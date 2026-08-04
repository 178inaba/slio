package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	s, err := Open("myws")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return s
}

func TestDirRespectsXDGCacheHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	got, err := Dir("myws")
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	want := filepath.Join(dir, "slio", "myws")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestChannelIDByNameMissOnEmptyCache(t *testing.T) {
	s := newTestStore(t)

	_, ok, err := s.ChannelIDByName("general", time.Now())
	if err != nil {
		t.Fatalf("ChannelIDByName() error = %v", err)
	}
	if ok {
		t.Error("ChannelIDByName() ok = true, want false on empty cache")
	}
}

func TestPutChannelsThenLookupHit(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	if err := s.PutChannels([]ChannelInfo{
		{ID: "C1", Name: "general"},
		{ID: "C2", Name: "random"},
	}, now); err != nil {
		t.Fatalf("PutChannels() error = %v", err)
	}

	id, ok, err := s.ChannelIDByName("random", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("ChannelIDByName() error = %v", err)
	}
	if !ok || id != "C2" {
		t.Errorf("ChannelIDByName() = (%q, %v), want (C2, true)", id, ok)
	}
}

func TestChannelIDByNameStaleEntryIsMiss(t *testing.T) {
	s := newTestStore(t)
	fetchedAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	if err := s.PutChannels([]ChannelInfo{{ID: "C1", Name: "general"}}, fetchedAt); err != nil {
		t.Fatalf("PutChannels() error = %v", err)
	}

	_, ok, err := s.ChannelIDByName("general", fetchedAt.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("ChannelIDByName() error = %v", err)
	}
	if ok {
		t.Error("ChannelIDByName() ok = true, want false for an entry past the 24h TTL")
	}
}

func TestPutChannelsReplacesPreviousListing(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	if err := s.PutChannels([]ChannelInfo{{ID: "C1", Name: "general"}}, now); err != nil {
		t.Fatalf("PutChannels() error = %v", err)
	}
	if err := s.PutChannels([]ChannelInfo{{ID: "C2", Name: "random"}}, now); err != nil {
		t.Fatalf("PutChannels() error = %v", err)
	}

	if _, ok, _ := s.ChannelIDByName("general", now); ok {
		t.Error("ChannelIDByName(general) ok = true, want false after replacement")
	}
	if id, ok, _ := s.ChannelIDByName("random", now); !ok || id != "C2" {
		t.Errorf("ChannelIDByName(random) = (%q, %v), want (C2, true)", id, ok)
	}
}

func TestUserDisplayNameMissOnEmptyCache(t *testing.T) {
	s := newTestStore(t)

	_, ok, err := s.UserDisplayName("U1", time.Now())
	if err != nil {
		t.Fatalf("UserDisplayName() error = %v", err)
	}
	if ok {
		t.Error("UserDisplayName() ok = true, want false on empty cache")
	}
}

func TestPutUserThenLookupHit(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	if err := s.PutUser("U1", "Alice", now); err != nil {
		t.Fatalf("PutUser() error = %v", err)
	}

	name, ok, err := s.UserDisplayName("U1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("UserDisplayName() error = %v", err)
	}
	if !ok || name != "Alice" {
		t.Errorf("UserDisplayName() = (%q, %v), want (Alice, true)", name, ok)
	}
}

func TestPutUserUpsertDoesNotClobberOthers(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()

	if err := s.PutUser("U1", "Alice", now); err != nil {
		t.Fatalf("PutUser() error = %v", err)
	}
	if err := s.PutUser("U2", "Bob", now); err != nil {
		t.Fatalf("PutUser() error = %v", err)
	}

	name, ok, _ := s.UserDisplayName("U1", now)
	if !ok || name != "Alice" {
		t.Errorf("UserDisplayName(U1) = (%q, %v), want (Alice, true)", name, ok)
	}
}

func TestUserDisplayNameStaleEntryIsMiss(t *testing.T) {
	s := newTestStore(t)
	fetchedAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	if err := s.PutUser("U1", "Alice", fetchedAt); err != nil {
		t.Fatalf("PutUser() error = %v", err)
	}

	_, ok, err := s.UserDisplayName("U1", fetchedAt.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("UserDisplayName() error = %v", err)
	}
	if ok {
		t.Error("UserDisplayName() ok = true, want false for an entry past the 24h TTL")
	}
}

func TestPutChannelsCreatesCacheFileOnDisk(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutChannels([]ChannelInfo{{ID: "C1", Name: "general"}}, time.Now()); err != nil {
		t.Fatalf("PutChannels() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "channels.json")); err != nil {
		t.Errorf("stat channels.json: %v", err)
	}
}
