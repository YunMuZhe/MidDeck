//go:build headless

package main

import (
	"fmt"
	"os"

	"github.com/YunMuZhe/MidDeck/internal/config"
)

func init() { config.FileSecrets = true }

// The headless build has no window: it is the MCP server and the changeset CLI,
// for servers without a desktop (no GTK / WebKit needed).
func runGUI() int {
	fmt.Fprint(os.Stderr, `MidDeck (headless build) has no window. Commands:

  MidDeck mcp [--repo DIR|URL] [--data-dir DIR]   MCP server over stdio
  MidDeck changeset plan|apply|export ...          changesets, see "MidDeck changeset -h"
  MidDeck version
`)
	return 2
}
