package main

import (
	"log/slog"
	"time"

	coretelemetry "github.com/openbotstack/openbotstack-core/telemetry"
	"github.com/openbotstack/openbotstack-runtime/api"
	runtimetelemetry "github.com/openbotstack/openbotstack-runtime/telemetry"
	"github.com/openbotstack/openbotstack-runtime/telemetry/store"
	"github.com/openbotstack/openbotstack-runtime/toolrunner/tool_invocation"

	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

// InitTelemetry creates telemetry stores and wires instrumentor into harness hooks.
func (b *ServerBuilder) InitTelemetry() *ServerBuilder {
	b.requireInit("pdb", "InitTelemetry")
	spanStore := store.NewRingBufferSpanStore(1000)
	eventStore := store.NewRingBufferEventStore(500)
	meter := coretelemetry.NewMemoryMeter()
	telemetryInstrumentor := runtimetelemetry.NewInstrumentor(spanStore, eventStore, meter)
	telemetryHandler := api.NewTelemetryHandler(spanStore, eventStore, meter, telemetryInstrumentor)

	if b.hookMgr != nil {
		telemetryInstrumentor.RegisterHooks(b.hookMgr)
		slog.Info("telemetry instrumentor registered on harness")
	}

	httpAllowlistObj := wasm.NewHTTPAllowlist(b.cfg.Sandbox.HTTPAllowlist)
	sandboxedClient := wasm.NewSandboxedHTTPClientWithSSRF(httpAllowlistObj, nil)
	toolPipeline := tool_invocation.NewToolInvocationPipeline(sandboxedClient, b.registryClient, 30*time.Second)
	tool_invocation.WireHTTPFetch(b.hostFuncs, toolPipeline)
	slog.Info("tool invocation pipeline wired",
		"allowlist", b.cfg.Sandbox.HTTPAllowlist,
		"registry_url", b.cfg.Sandbox.ToolRegistryURL,
	)

	b.telemetry = telemetryHandler
	return b
}
