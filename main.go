package main

import (
	"os"

	"github.com/178inaba/slio/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
