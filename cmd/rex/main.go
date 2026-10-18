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
