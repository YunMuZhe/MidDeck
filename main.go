package main

import (
	"os"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp":
			os.Exit(runMCP(os.Args[2:]))
		case "changeset", "cs":
			os.Exit(runChangeset(os.Args[2:], os.Stdout, os.Stderr))
		case "version", "--version", "-v":
			os.Stdout.WriteString("MidDeck " + version + "\n")
			return
		}
	}
	os.Exit(runGUI())
}
