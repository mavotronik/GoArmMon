package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"goarmmon/internal/monitor"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	engine := monitor.NewEngine(*cfgPath)
	if err := engine.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "monitor: %v\n", err)
		os.Exit(1)
	}
}
