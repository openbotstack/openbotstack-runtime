package main

import (
	"fmt"
	"log/slog"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-runtime/internal/crypto"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// loadProvidersFromDB reads provider_config rows from SQLite, decrypts API keys
// (when an encryption key is configured), and registers each provider that has a
// non-empty key into the router. Returns the number of providers registered.
//
// Provider configuration is runtime-mutable state that lives in SQLite and is
// managed via the Admin API — NOT environment variables. Env vars are reserved
// for static, deploy-time config (listen address, DB path, JWT secret). On a
// fresh database the table is empty and no providers are registered; the
// operator configures them after startup via POST /v1/admin/providers.
func loadProvidersFromDB(pdb *persistence.DB, factory *providers.ProviderFactory, rtr *router.DefaultRouter) (int, error) {
	rows, err := pdb.Query(`SELECT id, provider, base_url, api_key, model FROM provider_config ORDER BY is_default DESC, provider`)
	if err != nil {
		return 0, fmt.Errorf("query provider_config: %w", err)
	}
	defer func() { _ = rows.Close() }()

	registered := 0
	for rows.Next() {
		var id, provider, baseURL, storedKey, model string
		if err := rows.Scan(&id, &provider, &baseURL, &storedKey, &model); err != nil {
			return registered, fmt.Errorf("scan provider row: %w", err)
		}

		// Decrypt the stored key if encryption is configured and the value is encrypted.
		// Plaintext values (encryption disabled, or pre-encryption rows) pass through.
		apiKey := storedKey
		if encKey := crypto.EncryptionKey(); encKey != nil && crypto.IsEncrypted(storedKey) {
			dec, decErr := crypto.Decrypt(encKey, storedKey)
			if decErr != nil {
				slog.Warn("failed to decrypt provider api key, skipping",
					"id", id, "provider", provider, "error", decErr)
				continue
			}
			apiKey = dec
		}

		if apiKey == "" {
			continue
		}

		p := factory.Create(provider, baseURL, apiKey, model)
		if regErr := rtr.Register(p); regErr != nil {
			slog.Warn("failed to register provider",
				"id", id, "provider", provider, "error", regErr)
			continue
		}
		registered++
		slog.Info("provider registered from database",
			"id", id, "provider", provider, "model", model, "base_url", baseURL)
	}
	return registered, rows.Err()
}
