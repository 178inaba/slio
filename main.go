// Command slio reads Slack threads, channel history and search results
// from the command line, in a form an AI coding agent can consume without
// a screenshot. It is a thin entry point: everything the CLI does lives in
// internal/cmd, which returns the exit code rather than calling os.Exit,
// so the whole command tree stays testable.
package main

import (
	"os"

	"github.com/178inaba/slio/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
