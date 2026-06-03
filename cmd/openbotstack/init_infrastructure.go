package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/openbotstack/openbotstack-runtime/config"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// InitInfrastructure loads config, sets up logging, OpenTelemetry, and SQLite.
func (b *ServerBuilder) InitInfrastructure() *ServerBuilder {
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logLevel := parseLogLevel(cfg.Observability.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	otelCleanup, err := observability.Setup(context.Background(), cfg.Observability, "dev")
	if err != nil {
		slog.Error("failed to initialize OpenTelemetry", "error", err)
		os.Exit(1)
	}
	if err := observability.InitMetrics(); err != nil {
		slog.Error("failed to initialize OTel metrics", "error", err)
		os.Exit(1)
	}
	if err := observability.InitAppMetrics(); err != nil {
		slog.Error("failed to initialize app metrics", "error", err)
		os.Exit(1)
	}

	if *listenAddr != ":8080" {
		cfg.Server.Addr = *listenAddr
	}

	slog.Info("starting openbotstack",
		"addr", cfg.Server.Addr,
		"mode", *runMode,
		"llm_provider", cfg.Providers.LLM.Default,
	)

	dbPath := os.Getenv("OBS_DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/openbotstack.db"
	}
	pdb, err := persistence.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	if err := pdb.Migrate(); err != nil {
		slog.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateSignatureColumn(); err != nil {
		slog.Error("failed to migrate signature column", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateStepContextColumns(); err != nil {
		slog.Error("failed to migrate step context columns", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateAPIKeyRoleColumn(); err != nil {
		slog.Error("failed to migrate api key role column", "error", err)
		os.Exit(1)
	}
	slog.Info("sqlite database initialized", "path", dbPath)

	if os.Getenv("OBS_SEED_DEFAULTS") != "false" {
		seedKey, err := pdb.SeedDefaults()
		if err != nil {
			slog.Error("failed to seed defaults", "error", err)
			os.Exit(1)
		}
		if seedKey != "" {
			fmt.Println("warning: Default admin API Key (save this, it won't be shown again):")
			fmt.Printf("    %s\n", seedKey)
			fmt.Println()
			fmt.Println("    Tenant: default  User: admin  Role: admin")
		}
	}

	b.cfg = cfg
	b.pdb = pdb
	b.otelCleanup = otelCleanup
	return b
}
