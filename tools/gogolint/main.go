// Command gogolint runs gogo's project-specific cops, written with
// rubocop-go. These rules encode invariants documented in AGENTS.md that
// golangci-lint cannot express — see the cops package for the catalog.
//
//	go run ./tools/gogolint [path...]   # defaults to the repo root
package main

import (
	"fmt"
	"os"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/runner"

	"github.com/dgageot/gogo/tools/gogolint/cops"
)

func main() {
	paths := os.Args[1:]
	if len(paths) == 0 {
		paths = []string{"."}
	}

	r := runner.New(cops.All(), config.DefaultConfig(), os.Stdout)
	count, err := r.Run(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gogolint: %v\n", err)
		os.Exit(1)
	}
	if count > 0 {
		os.Exit(1)
	}
}
