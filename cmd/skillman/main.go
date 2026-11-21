// Command skillman: TUI and CLI for a cold skill library of coding-agent skills.
// Invoked as skill-add or skill-search (symlinks), it behaves like those legacy tools.
package main

import (
	"os"

	"github.com/hoangisreal/skillman/internal/cli"
	"github.com/hoangisreal/skillman/internal/tui"
)

func main() {
	cli.TUI = tui.Run
	os.Exit(cli.Run(os.Args[0], os.Args[1:]))
}
