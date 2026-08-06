// cmd_query implements the "query" subcommand: reads sync.yaml and outputs
// the configuration for a given component as JSON.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func cmdQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	configPath := fs.String("config", "component-charts/sync.yaml", "Path to sync config file")
	componentName := fs.String("component", "", "component name (required)")
	fs.Parse(args)

	if *componentName == "" {
		fmt.Fprintf(os.Stderr, "Usage: sync-components query --component NAME [--config PATH]\n")
		os.Exit(1)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	c, ok := cfg.Components[*componentName]
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: component %q not found in config\n", *componentName)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(c); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}
