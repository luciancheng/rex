// Command rex is a terminal sidecar for coding agents: plans in
// rex/plans/, a live view of agent hook events, the pinned task handed to
// the agent before each prompt, and a pixel-art dinosaur. Run "rex start"
// in a repo.
package main

import (
	"os"

	"github.com/luciancheng/rex/internal/cli"
	"github.com/luciancheng/rex/internal/tui"
)

func main() {
	code := cli.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if code == 130 {
		tui.RestoreOnInterrupt(os.Stdout)
	}
	os.Exit(code)
}
