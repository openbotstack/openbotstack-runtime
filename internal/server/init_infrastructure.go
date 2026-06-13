package server

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
	if b.err != nil {
		return b
	}

	cfg, err := config.Load(b.opts.ConfigPath)
	if err != nil {
		b.fail("failed to load config", err)
		return b
	}

	logLevel := parseLogLevel(cfg.Observability.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	otelCleanup, err := observability.Setup(context.Background(), cfg.Observability, "dev")
	if err != nil {
		b.fail("failed to initialize OpenTelemetry", err)
		return b
	}
	if err := observability.InitMetrics(); err != nil {
		b.fail("failed to initialize OTel metrics", err)
		return b
	}
	if err := observability.InitAppMetrics(); err != nil {
		b.fail("failed to initialize app metrics", err)
		return b
	}

	if b.opts.ListenAddr != ":8080" {
		cfg.Server.Addr = b.opts.ListenAddr
	}

	slog.Info("starting openbotstack",
		"addr", cfg.Server.Addr,
		"mode", b.opts.RunMode,
	)

	dbPath := os.Getenv("OBS_DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/openbotstack.db"
	}
	pdb, err := persistence.Open(dbPath)
	if err != nil {
		b.fail("failed to open database", err)
		return b
	}
	if err := pdb.Migrate(); err != nil {
		b.fail("failed to migrate database", err)
		return b
	}
	if err := pdb.MigrateSignatureColumn(); err != nil {
		b.fail("failed to migrate signature column", err)
		return b
	}
	if err := pdb.MigrateStepContextColumns(); err != nil {
		b.fail("failed to migrate step context columns", err)
		return b
	}
	if err := pdb.MigrateAPIKeyRoleColumn(); err != nil {
		b.fail("failed to migrate api key role column", err)
		return b
	}
	slog.Info("sqlite database initialized", "path", dbPath)

	if os.Getenv("OBS_SEED_DEFAULTS") != "false" {
		seedKey, err := pdb.SeedDefaults()
		if err != nil {
			b.fail("failed to seed defaults", err)
			return b
		}
		if seedKey != "" {
			slog.Warn("default admin API key generated (save it, it won't be shown again)",
				"tenant", "default", "user", "admin", "role", "admin")
			// Printed to stdout so first-run operators can capture it.
			fmt.Println("warning: Default admin API Key (save this, it won't be shown again):")
			fmt.Println("    " + seedKey)
			fmt.Println("    Tenant: default  User: admin  Role: admin")
		}
	}

	b.cfg = cfg
	b.pdb = pdb
	b.otelCleanup = otelCleanup
	return b
}
