package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage(1)
	}

	switch os.Args[1] {
	case "sync":
		cmdSync(os.Args[2:])
	case "find-targets":
		cmdFindTargets(os.Args[2:])
	case "query":
		cmdQuery(os.Args[2:])
	case "help", "--help", "-h":
		usage(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		usage(1)
	}
}

func usage(exitCode int) {
	fmt.Fprintf(os.Stderr, `Usage: sync-components <command> [flags]

Commands:
  sync           Sync component Helm charts from upstream repos
  find-targets   Find branches tracking a given component ref
  query          Output tracking info for a component as JSON

Run 'sync-components <command> --help' for details on a specific command.
`)
	os.Exit(exitCode)
}
