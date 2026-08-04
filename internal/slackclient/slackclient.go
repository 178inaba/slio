// Package slackclient wraps github.com/slack-go/slack with the deadline and
// rate-limit handling slio's commands need: every call runs against a
// caller-supplied context and, on HTTP 429, retries honoring Retry-After as
// long as the context's deadline allows.
package slackclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/slack-go/slack"
)

// Client is a deadline- and retry-aware wrapper around *slack.Client.
type Client struct {
	api *slack.Client
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
	return &Client{api: slack.New(token, slackOpts...)}
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

// withRetry runs fn, retrying while it fails with a rate-limit error and
// honoring the Retry-After duration, as long as ctx's deadline (if any)
// allows another attempt. A deadline that would pass before the wait
// completes is reported as a clear error rather than left to a partial,
// silently-abandoned attempt.
func withRetry(ctx context.Context, fn func() error) error {
	for {
		err := fn()

		var rlErr *slack.RateLimitedError
		if !errors.As(err, &rlErr) {
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
