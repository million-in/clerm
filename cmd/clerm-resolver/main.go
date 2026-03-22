package main

import (
	"os"

	"github.com/million-in/clerm/internal/app/resolvercli"
	"github.com/million-in/clerm/internal/platform"
)

func main() {
	logger, err := platform.NewLogger("info")
	if err != nil {
		os.Exit(1)
	}
	if err := resolvercli.Run(logger, os.Args[1:]); err != nil {
		platform.LogError(logger, "clerm resolver failed", err)
		os.Exit(1)
	}
}
