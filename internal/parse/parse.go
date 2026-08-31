// Package parse turns slio's raw CLI arguments (thread URLs, channel
// references, --since/--until values) into structured data.
package parse

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// The digits after "p" require at least 7: a real Slack ts is 10 digits
	// of epoch seconds plus 6 of fractional microseconds, and the dot
	// always sits 6 digits from the end (see tsFromPermalinkDigits) — with
	// fewer than 7 digits that split would go negative.
	permalinkPathRe = regexp.MustCompile(`^/archives/([A-Za-z0-9]+)/p(\d{7,})$`)
	channelPathRe   = regexp.MustCompile(`^/archives/([A-Za-z0-9]+)`)
)

// ThreadRef identifies a single Slack thread: the channel it lives in, its
// workspace host, the timestamp to fetch the thread from, and the timestamp
// of the one message the URL pointed at.
type ThreadRef struct {
	Host    string
	Channel string
	// Ts is the thread to fetch: the parent message's timestamp.
	Ts string
	// TargetTs is the timestamp of the message the URL pointed at, taken
	// from the permalink's p<digits> segment. It equals Ts for a parent
	// permalink and differs for a reply permalink.
	TargetTs string
}

// ThreadURL parses a Slack message permalink into a ThreadRef. It accepts
// both the canonical form (.../archives/<channel>/p<digits>) and a reply
// permalink carrying ?thread_ts=<ts>&cid=<channel>. The two carry different
// information and both parts are kept: thread_ts (when present) is the
// thread to fetch, while the p<digits> segment always names the message the
// URL pointed at. Passing a reply's ts to conversations.replies would
// return that reply alone rather than the thread it belongs to — which is
// also why a reply permalink stripped of its ?thread_ts= (not a form
// Slack's "Copy link" produces) resolves to that reply alone, leaving it
// the only message `slio thread` has to mark.
func ThreadURL(raw string) (ThreadRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ThreadRef{}, fmt.Errorf("parse thread URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return ThreadRef{}, fmt.Errorf("%q is not an absolute URL", raw)
	}

	m := permalinkPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return ThreadRef{}, fmt.Errorf(
			"%q is not a Slack message permalink (expected .../archives/<channel>/p<digits>)", raw)
	}
	channel, targetTs := m[1], tsFromPermalinkDigits(m[2])
	ts := targetTs

	if threadTs := u.Query().Get("thread_ts"); threadTs != "" {
		ts = threadTs
		if cid := u.Query().Get("cid"); cid != "" {
			channel = cid
		}
	}

	return ThreadRef{Host: u.Host, Channel: channel, Ts: ts, TargetTs: targetTs}, nil
}

// tsFromPermalinkDigits converts a permalink's "p<digits>" segment back into
// a Slack message ts, e.g. "1234567890123456" -> "1234567890.123456" (the
// dot is always 6 digits from the end).
func tsFromPermalinkDigits(digits string) string {
	return digits[:len(digits)-6] + "." + digits[len(digits)-6:]
}

// Channel is a parsed history/thread channel argument: a URL or bare
// channel/DM/group-DM ID (ID set, Host set only for the URL form), or a
// "#name" reference (Name set, left for the caller to resolve via the
// channel cache).
type Channel struct {
	Host string
	ID   string
	Name string
}

// ChannelArg parses the argument to `slio history`: a Slack URL, a
// bare channel/DM/group-DM ID, or a "#name" reference.
func ChannelArg(raw string) (Channel, error) {
	switch {
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"):
		u, err := url.Parse(raw)
		if err != nil {
			return Channel{}, fmt.Errorf("parse channel URL %q: %w", raw, err)
		}
		m := channelPathRe.FindStringSubmatch(u.Path)
		if m == nil {
			return Channel{}, fmt.Errorf(
				"%q does not point at a Slack channel (expected .../archives/<channel>)", raw)
		}
		return Channel{Host: u.Host, ID: m[1]}, nil

	case strings.HasPrefix(raw, "#"):
		name := strings.TrimPrefix(raw, "#")
		if name == "" {
			return Channel{}, fmt.Errorf("channel name is empty after %q", "#")
		}
		return Channel{Name: name}, nil

	case raw == "":
		return Channel{}, fmt.Errorf("channel argument is empty")

	default:
		return Channel{ID: raw}, nil
	}
}

var relativeDurationRe = regexp.MustCompile(`^(\d+)([dw])$`)

var (
	// isoZonedLayouts carry an explicit zone, so the instant they parse to
	// doesn't depend on the caller's location.
	isoZonedLayouts = []string{
		time.RFC3339,
		"2006-01-02T15:04Z07:00",
	}
	// isoLocalLayouts have no zone; a caller-supplied location applies.
	isoLocalLayouts = []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
)

// Time parses a --since/--until value relative to now: ISO 8601 (with
// or without a time-of-day component; no zone means now's location) or a
// relative duration meaning "that long before now" (30m, 24h, 7d, 2w). Go's
// time.ParseDuration doesn't support d/w, so only those two units are
// hand-rolled; everything else is delegated to it.
func Time(raw string, now time.Time) (time.Time, error) {
	if m := relativeDurationRe.FindStringSubmatch(raw); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, fmt.Errorf("parse relative duration %q: %w", raw, err)
		}
		unit := 24 * time.Hour
		if m[2] == "w" {
			unit = 7 * 24 * time.Hour
		}
		return now.Add(-time.Duration(n) * unit), nil
	}

	if d, err := time.ParseDuration(raw); err == nil {
		return now.Add(-d), nil
	}

	for _, layout := range isoZonedLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	for _, layout := range isoLocalLayouts {
		if t, err := time.ParseInLocation(layout, raw, now.Location()); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf(
		"%q is not a valid ISO 8601 timestamp or relative duration (e.g. 24h, 7d, 2w)", raw)
}
