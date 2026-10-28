package tui_test

import (
	"github.com/luciancheng/rex/internal/cli"
	"github.com/luciancheng/rex/internal/tui"
)

// The human CUJ hands a prompt hook's payload to rex ingest itself.
// internal/cli imports this package, so only an external test can link it
// in; it does so before any test runs.
func init() { tui.IngestRun = cli.Main }
