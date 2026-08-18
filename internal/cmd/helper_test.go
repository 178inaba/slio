package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// errorMarker is how a report opens once Fprintln has joined errorPrefix to
// the message with a space. errorLine searches for it.
const errorMarker = errorPrefix + " "

// runSlio runs slio the way the binary does — through Execute — and reports
// the exit code alongside what each stream received. Driving Execute rather
// than the command tree is what makes the exit code observable, and passing
// the arguments in is what makes them reach real flag parsing: a root with
// no argument list falls back to os.Args[1:], which would feed `go test`'s
// own flags to slio.
func runSlio(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	// An empty reader rather than nil: cobra falls back to the process
	// stdin when no reader is set, which would make these tests depend on
	// how they were started. No command reached this way reads stdin.
	return runSlioWithStdin(t, strings.NewReader(""), args...)
}

// runSlioWithStdin is runSlio with a scriptable stdin, for the commands that
// prompt. A non-*os.File reader also reads as "not a terminal", which is the
// seam `auth login` uses to skip the masked prompt.
func runSlioWithStdin(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Execute(args, stdin, &out, &errOut)
	return out.String(), errOut.String(), code
}

// errorLine returns the failure slio reported, from the "Error: " marker to
// the end of that line, and fails the test unless stderr carries exactly one
// marker — which is how every caller also pins that a failure is reported
// once rather than twice.
//
// Slicing from the marker rather than taking the whole line is required: a
// prompt written with Fprint leaves no newline, so the report is glued to
// it. `auth login` produces
//
//	Paste your Slack user OAuth token (xoxp-...): Error: token must start with "xoxp-"; …
//
// on one line, and asserting against the whole stream would let a check for
// "xoxp-" pass with no error reported at all.
func errorLine(t *testing.T, stderr string) string {
	t.Helper()
	if got := strings.Count(stderr, errorMarker); got != 1 {
		t.Fatalf("stderr carries %d %q markers, want exactly 1: %q", got, errorMarker, stderr)
	}
	line := stderr[strings.Index(stderr, errorMarker):]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	return line
}
