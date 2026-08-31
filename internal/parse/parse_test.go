package parse

import (
	"testing"
	"time"
)

func TestThreadURLCanonicalPermalink(t *testing.T) {
	got, err := ThreadURL("https://myws.slack.com/archives/C0123456789/p1234567890123456")
	if err != nil {
		t.Fatalf("ThreadURL() error = %v", err)
	}
	want := ThreadRef{
		Host: "myws.slack.com", Channel: "C0123456789",
		Ts: "1234567890.123456", TargetTs: "1234567890.123456",
	}
	if got != want {
		t.Errorf("ThreadURL() = %+v, want %+v", got, want)
	}
}

func TestThreadURLReplyPermalinkWithThreadTsAndCid(t *testing.T) {
	got, err := ThreadURL("https://myws.slack.com/archives/C0123456789/p1111111111222222?thread_ts=1234567890.123456&cid=C0123456789")
	if err != nil {
		t.Fatalf("ThreadURL() error = %v", err)
	}
	want := ThreadRef{
		Host: "myws.slack.com", Channel: "C0123456789",
		Ts: "1234567890.123456", TargetTs: "1111111111.222222",
	}
	if got != want {
		t.Errorf("ThreadURL() = %+v, want %+v", got, want)
	}
}

func TestThreadURLReplyPermalinkWithThreadTsOnly(t *testing.T) {
	got, err := ThreadURL("https://myws.slack.com/archives/C0123456789/p1111111111222222?thread_ts=1234567890.123456")
	if err != nil {
		t.Fatalf("ThreadURL() error = %v", err)
	}
	if got.Channel != "C0123456789" {
		t.Errorf("Channel = %q, want fallback to path channel C0123456789", got.Channel)
	}
	if got.Ts != "1234567890.123456" {
		t.Errorf("Ts = %q, want the thread_ts value", got.Ts)
	}
	if got.TargetTs != "1111111111.222222" {
		t.Errorf("TargetTs = %q, want the p<digits> ts", got.TargetTs)
	}
}

func TestThreadURLInvalidPath(t *testing.T) {
	if _, err := ThreadURL("https://myws.slack.com/some/other/path"); err == nil {
		t.Fatal("ThreadURL() error = nil, want error")
	}
}

func TestThreadURLNotAbsolute(t *testing.T) {
	if _, err := ThreadURL("archives/C0123456789/p1234567890123456"); err == nil {
		t.Fatal("ThreadURL() error = nil, want error")
	}
}

func TestThreadURLTooFewDigitsReturnsErrorNotPanic(t *testing.T) {
	if _, err := ThreadURL("https://myws.slack.com/archives/C0123456789/p123"); err == nil {
		t.Fatal("ThreadURL() error = nil, want error for a too-short p<digits> segment")
	}
}

func TestChannelArgURL(t *testing.T) {
	got, err := ChannelArg("https://myws.slack.com/archives/C0123456789")
	if err != nil {
		t.Fatalf("ChannelArg() error = %v", err)
	}
	want := Channel{Host: "myws.slack.com", ID: "C0123456789"}
	if got != want {
		t.Errorf("ChannelArg() = %+v, want %+v", got, want)
	}
}

func TestChannelArgURLWithTrailingSegments(t *testing.T) {
	got, err := ChannelArg("https://myws.slack.com/archives/C0123456789/p1234567890123456")
	if err != nil {
		t.Fatalf("ChannelArg() error = %v", err)
	}
	if got.ID != "C0123456789" || got.Host != "myws.slack.com" {
		t.Errorf("ChannelArg() = %+v, want ID=C0123456789 Host=myws.slack.com", got)
	}
}

func TestChannelArgBareID(t *testing.T) {
	got, err := ChannelArg("D0123456789")
	if err != nil {
		t.Fatalf("ChannelArg() error = %v", err)
	}
	want := Channel{ID: "D0123456789"}
	if got != want {
		t.Errorf("ChannelArg() = %+v, want %+v", got, want)
	}
}

func TestChannelArgName(t *testing.T) {
	got, err := ChannelArg("#general")
	if err != nil {
		t.Fatalf("ChannelArg() error = %v", err)
	}
	want := Channel{Name: "general"}
	if got != want {
		t.Errorf("ChannelArg() = %+v, want %+v", got, want)
	}
}

func TestChannelArgEmptyName(t *testing.T) {
	if _, err := ChannelArg("#"); err == nil {
		t.Fatal("ChannelArg() error = nil, want error")
	}
}

func TestChannelArgEmpty(t *testing.T) {
	if _, err := ChannelArg(""); err == nil {
		t.Fatal("ChannelArg() error = nil, want error")
	}
}

func TestTimeISO8601(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		raw  string
		want time.Time
	}{
		{"2026-08-01", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-08-01T10:00", time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)},
		{"2026-08-01T10:00:00", time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := Time(tt.raw, now)
			if err != nil {
				t.Fatalf("Time(%q) error = %v", tt.raw, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("Time(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestTimeISO8601WithZoneIsInstantExact(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.FixedZone("JST", 9*3600))

	got, err := Time("2026-08-01T10:00:00+09:00", now)
	if err != nil {
		t.Fatalf("Time() error = %v", err)
	}
	want := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("Time() = %v, want instant equal to %v", got, want)
	}
}

func TestTimeRelative(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		raw  string
		want time.Time
	}{
		{"30m", now.Add(-30 * time.Minute)},
		{"24h", now.Add(-24 * time.Hour)},
		{"7d", now.Add(-7 * 24 * time.Hour)},
		{"2w", now.Add(-2 * 7 * 24 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := Time(tt.raw, now)
			if err != nil {
				t.Fatalf("Time(%q) error = %v", tt.raw, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("Time(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestTimeInvalid(t *testing.T) {
	if _, err := Time("yesterday", time.Now()); err == nil {
		t.Fatal("Time() error = nil, want error")
	}
}
