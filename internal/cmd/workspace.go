package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/178inaba/slio/internal/config"
)

// resolveWorkspace resolves credentials for a command invocation and, when
// SLIO_TOKEN bypassed profile resolution, calls auth.test once to learn
// the workspace host (for permalinks) and a cache key derived from its
// team ID (since no profile name is available to key the cache by).
// profile is the --profile flag value, and urlHost is the host parsed from
// the command's URL argument, or "" for commands that don't take one
// (search, channel list).
func resolveWorkspace(ctx context.Context, profile, urlHost string) (creds config.Credentials, host, cacheKey string, err error) {
	file, err := config.Load()
	if err != nil {
		return config.Credentials{}, "", "", err
	}
	creds, err = config.Resolve(file, profile, urlHost, os.Getenv)
	if err != nil {
		return config.Credentials{}, "", "", err
	}
	if !creds.ViaEnvToken {
		return creds, creds.Host, creds.Profile, nil
	}

	result, err := slackClientFactory(creds.Token).AuthTest(ctx)
	if err != nil {
		return config.Credentials{}, "", "", fmt.Errorf("resolve workspace for SLIO_TOKEN: %w", err)
	}
	return creds, result.Host, "team-" + result.TeamID, nil
}
