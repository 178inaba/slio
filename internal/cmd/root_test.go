package cmd

import (
	"strings"
	"testing"
)

// TestInvalidFormatIsRejected covers the wiring of the --format validation:
// it runs as the root's PersistentPreRunE, so a subcommand must reject a bad
// value before doing any work. A command that reaches the API would fail the
// test by connecting to the real Slack endpoint.
func TestInvalidFormatIsRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, _, err := runSlio(t, "channel", "list", "--format", "yaml")
	if err == nil {
		t.Fatal("slio channel list --format yaml: error = nil, want error for an unsupported --format")
	}
	if !strings.Contains(err.Error(), "invalid --format") {
		t.Errorf("error = %v, want it to report an invalid --format", err)
	}
}
