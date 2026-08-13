package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

// newTestRoot returns a freshly built command tree wired to separate stdout
// and stderr buffers, so tests can assert which stream a message went to.
// Building the tree per test keeps flag values from leaking between cases.
// Prefer runSlio; this is for tests that also need to script stdin.
func newTestRoot(t *testing.T) (root *cobra.Command, stdout, stderr *bytes.Buffer) {
	t.Helper()
	testRoot := newRootCmd(&globalFlags{})
	var out, errOut bytes.Buffer
	testRoot.SetOut(&out)
	testRoot.SetErr(&errOut)
	return testRoot, &out, &errOut
}

// runSlio builds a fresh command tree, runs it with args, and reports what
// each stream received. Going through SetArgs is what makes the arguments
// reach real flag parsing — a root with no argument list falls back to
// os.Args[1:], which would feed `go test`'s own flags to slio.
func runSlio(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root, out, errOut := newTestRoot(t)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}
