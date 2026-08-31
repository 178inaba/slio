// Command slio is the entry point for the slio CLI; the command tree and
// everything it does live in internal/cmd.
package main

import (
	"os"

	"github.com/178inaba/slio/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
