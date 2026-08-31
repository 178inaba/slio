// Package slackclient wraps github.com/slack-go/slack with the deadline and
// rate-limit handling slio's commands need: every call runs against a
// caller-supplied context and, on HTTP 429, retries honoring Retry-After as
// long as the context's deadline allows.
package slackclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// Client is a deadline- and retry-aware wrapper around *slack.Client.
type Client struct {
	api   *slack.Client
	token string
}

// Option configures a Client constructed by New.
type Option func(*clientConfig)

type clientConfig struct {
	apiURL string
}

// WithAPIURL overrides the Slack API base URL. Tests use this to point the
// client at an httptest server; production callers should leave it unset.
func WithAPIURL(apiURL string) Option {
	return func(c *clientConfig) { c.apiURL = apiURL }
}

// New creates a Client authenticated with the given user token.
func New(token string, opts ...Option) *Client {
	var cfg clientConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	var slackOpts []slack.Option
	if cfg.apiURL != "" {
		slackOpts = append(slackOpts, slack.OptionAPIURL(cfg.apiURL))
	}
	return &Client{api: slack.New(token, slackOpts...), token: token}
}

// AuthTestResult is the subset of auth.test's response slio needs.
type AuthTestResult struct {
	// Host is the workspace's host (e.g. "myws.slack.com"), used both to
	// record a profile's workspace and to build thread permalinks.
	Host string
	// TeamID keys the local cache when SLIO_TOKEN bypasses profile
	// resolution and no profile name is available.
	TeamID string
}

// AuthTest verifies the client's token and reports the workspace it
// belongs to.
func (c *Client) AuthTest(ctx context.Context) (AuthTestResult, error) {
	var resp *slack.AuthTestResponse
	err := withRetry(ctx, func() error {
		var err error
		resp, err = c.api.AuthTestContext(ctx)
		return err
	})
	if err != nil {
		return AuthTestResult{}, err
	}

	host, err := hostFromURL(resp.URL)
	if err != nil {
		return AuthTestResult{}, err
	}
	return AuthTestResult{Host: host, TeamID: resp.TeamID}, nil
}

func hostFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse workspace url %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("workspace url %q has no host", rawURL)
	}
	return u.Host, nil
}

// ConversationReplies fetches every message in a thread (the parent plus
// all replies), following cursor pagination until Slack reports no more.
func (c *Client) ConversationReplies(ctx context.Context, channel, ts string) ([]slack.Message, error) {
	var all []slack.Message
	cursor := ""
	for {
		params := &slack.GetConversationRepliesParameters{
			ChannelID: channel,
			Timestamp: ts,
			Cursor:    cursor,
		}

		var msgs []slack.Message
		var hasMore bool
		var nextCursor string
		err := withRetry(ctx, func() error {
			var err error
			msgs, hasMore, nextCursor, err = c.api.GetConversationRepliesContext(ctx, params)
			return err
		})
		if err != nil {
			return nil, err
		}

		all = append(all, msgs...)
		if !hasMore || nextCursor == "" {
			return all, nil
		}
		cursor = nextCursor
	}
}

// ConversationHistory fetches up to limit of the most recent messages in a
// channel (optionally bounded by oldest/latest, both Slack ts strings, or
// "" for no bound), following cursor pagination as needed. hasMore reports
// whether more messages exist beyond what was returned, so callers can
// show an "older messages omitted" notice; it's determined by asking for
// one extra message and dropping it if present, rather than trusting
// Slack's has_more on the exact boundary page.
func (c *Client) ConversationHistory(ctx context.Context, channel, oldest, latest string, limit int) (messages []slack.Message, hasMore bool, err error) {
	// conversations.history documents 999 as the max per-request limit;
	// behavior above that is undocumented, so each page is capped there
	// regardless of how large the caller's overall limit is. This is safe
	// to vary per page (unlike SearchMessages' count) because cursor
	// pagination, not a fixed page size, defines the boundaries.
	const maxPageLimit = 999
	target := limit + 1
	var all []slack.Message
	cursor := ""
	for len(all) < target {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channel,
			Oldest:    oldest,
			Latest:    latest,
			Cursor:    cursor,
			Limit:     min(target-len(all), maxPageLimit),
		}

		var resp *slack.GetConversationHistoryResponse
		err := withRetry(ctx, func() error {
			var err error
			resp, err = c.api.GetConversationHistoryContext(ctx, params)
			return err
		})
		if err != nil {
			return nil, false, err
		}

		all = append(all, resp.Messages...)
		if !resp.HasMore || resp.ResponseMetaData.NextCursor == "" {
			break
		}
		cursor = resp.ResponseMetaData.NextCursor
	}

	if len(all) > limit {
		return all[:limit], true, nil
	}
	return all, false, nil
}

// ConversationsForUser lists the public and private channels the token's
// user is a member of, excluding archived ones, following cursor
// pagination. conversations.list can't filter by membership, hence
// users.conversations here instead.
func (c *Client) ConversationsForUser(ctx context.Context) ([]slack.Channel, error) {
	var all []slack.Channel
	cursor := ""
	for {
		params := &slack.GetConversationsForUserParameters{
			Types:           []string{"public_channel", "private_channel"},
			ExcludeArchived: true,
			Cursor:          cursor,
		}

		var channels []slack.Channel
		var nextCursor string
		err := withRetry(ctx, func() error {
			var err error
			channels, nextCursor, err = c.api.GetConversationsForUserContext(ctx, params)
			return err
		})
		if err != nil {
			return nil, err
		}

		all = append(all, channels...)
		if nextCursor == "" {
			return all, nil
		}
		cursor = nextCursor
	}
}

// SearchMessages searches messages, following Slack's page-number-based
// pagination (search.messages has no cursor) until limit results are
// collected or no more pages remain. total is the API's reported total
// hit count, for the "N more results" notice.
func (c *Client) SearchMessages(ctx context.Context, query string, limit int) (matches []slack.SearchMessage, total int, err error) {
	// count must stay fixed across pages: Slack's "page" is defined
	// relative to count (page 2 at count=50 is a different slice of
	// results than page 2 at count=100), so shrinking it as limit is
	// approached would shift page boundaries and produce duplicates or
	// gaps. Truncating to limit at the end handles the excess instead.
	const pageSize = 100
	var all []slack.SearchMessage
	page := 1
	for len(all) < limit {
		params := slack.NewSearchParameters()
		params.Count = pageSize
		params.Page = page

		var resp *slack.SearchMessages
		err := withRetry(ctx, func() error {
			var err error
			resp, err = c.api.SearchMessagesContext(ctx, query, params)
			return err
		})
		if err != nil {
			return nil, 0, err
		}

		all = append(all, resp.Matches...)
		total = resp.Total
		if page >= resp.PageCount || len(resp.Matches) == 0 {
			break
		}
		page++
	}

	if len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

// DownloadFile downloads a Slack file's contents from its url_private,
// authenticating with the client's own token (url_private requires a
// bearer token; slio must do the download itself, since only it holds the
// token), and writes it to destPath. Slack returns an HTML sign-in page
// with HTTP 200 rather than an error when the token lacks files:read, so
// that case is detected via Content-Type and reported explicitly rather
// than silently saving the wrong content.
func (c *Client) DownloadFile(ctx context.Context, urlPrivate, destPath string) error {
	return withRetry(ctx, func() error {
		return c.downloadFileOnce(ctx, urlPrivate, destPath)
	})
}

func (c *Client) downloadFileOnce(ctx context.Context, urlPrivate, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlPrivate, nil)
	if err != nil {
		return fmt.Errorf("build download request for %s: %w", urlPrivate, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", urlPrivate, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := time.Second
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		return &slack.RateLimitedError{RetryAfter: retryAfter}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", urlPrivate, resp.Status)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return fmt.Errorf(
			"download %s: received an HTML sign-in page instead of the file — the token likely lacks the files:read scope",
			urlPrivate)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", destPath, err)
	}
	defer func() { _ = f.Close() }() // best-effort safety net for the early-return paths above

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	// Closing explicitly (rather than relying only on the deferred close)
	// surfaces a flush failure, which io.Copy succeeding wouldn't catch.
	if err := f.Close(); err != nil {
		return fmt.Errorf("finalize %s: %w", destPath, err)
	}
	return nil
}

// GetUserInfo fetches a user's profile, for resolving display names.
func (c *Client) GetUserInfo(ctx context.Context, userID string) (*slack.User, error) {
	var user *slack.User
	err := withRetry(ctx, func() error {
		var err error
		user, err = c.api.GetUserInfoContext(ctx, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}

// withRetry runs fn, retrying while it fails with a rate-limit error and
// honoring the Retry-After duration, as long as ctx's deadline (if any)
// allows another attempt. A deadline that would pass before the wait
// completes is reported as a clear error rather than left to a partial,
// silently-abandoned attempt.
func withRetry(ctx context.Context, fn func() error) error {
	for {
		err := fn()

		rlErr, ok := errors.AsType[*slack.RateLimitedError](err)
		if !ok {
			return err
		}

		timer := time.NewTimer(rlErr.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("rate limited by Slack and the deadline passed before the retry: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
