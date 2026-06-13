// Package main is the OpenBotStack runtime entrypoint.
//
// It is deliberately thin: flag parsing and a single call into the composition
// root (internal/server.Build). All phased initialization, wiring, and serving
// live in package server so they are importable and testable.
//
// Usage:
//
//	openbotstack [flags]
//
// Flags:
//
//	--config    Path to optional config file (default: ./config.yaml, not required)
//	--addr      Listen address (default: :8080)
//	--mode      Run mode: all, api, worker (default: all)
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/openbotstack/openbotstack-runtime/internal/server"
)

var (
	configPath = flag.String("config", "./config.yaml", "Path to optional config file (env vars take precedence)")
	listenAddr = flag.String("addr", ":8080", "Listen address")
	runMode    = flag.String("mode", "all", "Run mode: all, api, worker")

	// Build metadata injected via -ldflags.
	version   string
	commit    string
	branch    string
	buildTime string
)

func main() {
	flag.Parse()

	srv, err := server.Build(server.Options{
		ConfigPath: *configPath,
		ListenAddr: *listenAddr,
		RunMode:    *runMode,
		Version:    version,
		Commit:     commit,
		Branch:     branch,
		BuildTime:  buildTime,
	})
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer srv.Cleanup()

	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
