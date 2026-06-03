package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openbotstack/openbotstack-core/control/agent"
	agentpkg "github.com/openbotstack/openbotstack-runtime/agent"
	contextassembler "github.com/openbotstack/openbotstack-runtime/context"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// InitMemory sets up Markdown store, conversation store, memory bridge, and context assembler.
func (b *ServerBuilder) InitMemory() *ServerBuilder {
	sessionStateStore := memory.NewSQLiteSessionStateStore(b.pdb.DB,
		memory.WithStrictTenant(os.Getenv("OBS_AUTH_STRICT") == "true"),
	)

	markdownStore, err := memory.NewMarkdownMemoryStore(b.cfg.Memory.DataDir)
	if err != nil {
		slog.Error("failed to create markdown memory store", "error", err)
		os.Exit(1)
	}
	slog.Info("markdown memory store initialized", "data_dir", b.cfg.Memory.DataDir)

	var convStore agent.ConversationStore = markdownStore
	if b.cfg.Memory.SummaryEnabled {
		summarizer := memory.NewConversationSummarizer(markdownStore, b.modelRouter, b.cfg.Memory.SummaryThreshold)
		convStore = memory.NewSummarizingConversationStore(markdownStore, summarizer)
		slog.Info("conversation summarization enabled", "threshold", b.cfg.Memory.SummaryThreshold)
	}
	convStore = memory.NewDualWriteConversationStore(convStore, sessionStateStore)

	switch a := b.apiAgent.(type) {
	case *agentpkg.HarnessAgent:
		a.SetConversationStore(convStore)
		a.SetMaxHistoryMessages(b.cfg.Memory.MaxHistoryMessages)
	}

	memoryBridge := memory.NewMarkdownMemoryBridge(markdownStore, nil)

	if b.cfg.Vector.Enabled && b.cfg.Vector.DatabaseURL != "" {
		b.initVectorSearch(markdownStore, memoryBridge, convStore)
	} else {
		slog.Info("vector search disabled (keyword matching only)")
	}

	contextAssembler := contextassembler.NewRuntimeContextAssembler(b.exec, memoryBridge)

	// Create ConversationManager to consolidate history + memory retrieval,
	// preventing duplicate RetrieveSimilar calls between HarnessAgent and ContextAssembler.
	conversationMgr := memory.NewConversationManager(convStore, memoryBridge, b.cfg.Memory.MaxHistoryMessages)

	switch a := b.apiAgent.(type) {
	case *agentpkg.HarnessAgent:
		a.SetContextAssembler(contextAssembler)
		a.SetMemoryManager(memoryBridge)
		a.SetConversationManager(conversationMgr)
	}
	slog.Info("context assembler initialized")

	b.markdownStore = markdownStore
	b.sessionStore = sessionStateStore
	return b
}

// initVectorSearch initializes optional PostgreSQL + pgvector for semantic search.
func (b *ServerBuilder) initVectorSearch(markdownStore *memory.MarkdownMemoryStore, memoryBridge *memory.MarkdownMemoryBridge, convStore agent.ConversationStore) {
	pgPool, err := pgxpool.New(context.Background(), b.cfg.Vector.DatabaseURL)
	if err != nil {
		slog.Error("failed to parse vector database URL", "error", err)
		os.Exit(1)
	}
	if err := pgPool.Ping(context.Background()); err != nil {
		slog.Error("failed to connect to vector database", "error", err)
		pgPool.Close()
		os.Exit(1)
	}

	vectorStore := memory.NewPgVectorStore(pgPool, b.cfg.Vector.Dimensions)
	if err := vectorStore.Migrate(context.Background()); err != nil {
		slog.Error("failed to migrate vector store", "error", err)
		os.Exit(1)
	}

	embeddingSvc := memory.NewEmbeddingService(b.modelRouter, b.cfg.Vector.Model, b.cfg.Vector.Dimensions)
	memoryBridge.SetRetrievalStrategy(memory.NewVectorFirstStrategy(markdownStore, vectorStore, embeddingSvc))

	indexer := memory.NewAsyncEmbeddingIndexer(embeddingSvc, vectorStore)
	if summarizingStore, ok := convStore.(*memory.SummarizingConversationStore); ok {
		summarizingStore.SetIndexer(indexer)
	}
	slog.Info("vector search enabled", "model", b.cfg.Vector.Model, "dimensions", b.cfg.Vector.Dimensions)
}
