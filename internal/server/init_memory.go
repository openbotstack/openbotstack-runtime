package server

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	agentpkg "github.com/openbotstack/openbotstack-runtime/agent"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// Compile-time checks: HarnessAgent must satisfy both interfaces.
var _ coreagent.MemoryConfigurable = (*agentpkg.HarnessAgent)(nil)
var _ agentpkg.ConversationConfigurable = (*agentpkg.HarnessAgent)(nil)

// InitMemory sets up Markdown store, conversation store, memory bridge, and context assembler.
func (b *ServerBuilder) InitMemory() *ServerBuilder {
	if b.err != nil {
		return b
	}
	b.requireInit("pdb", "InitMemory")
	b.requireInit("apiAgent", "InitMemory")

	sessionStateStore := memory.NewSQLiteSessionStateStore(b.pdb.DB,
		memory.WithStrictTenant(os.Getenv("OBS_AUTH_STRICT") == "true"),
	)

	markdownStore, err := memory.NewMarkdownMemoryStore(b.cfg.Memory.DataDir)
	if err != nil {
		b.fail("failed to create markdown memory store", err)
		return b
	}
	slog.Info("markdown memory store initialized", "data_dir", b.cfg.Memory.DataDir)

	var convStore coreagent.ConversationStore = markdownStore
	var summarizingStore *memory.SummarizingConversationStore
	if b.cfg.Memory.SummaryEnabled {
		summarizer := memory.NewConversationSummarizer(markdownStore, b.modelRouter, b.cfg.Memory.SummaryThreshold)
		summarizingStore = memory.NewSummarizingConversationStore(markdownStore, summarizer)
		convStore = summarizingStore
		slog.Info("conversation summarization enabled", "threshold", b.cfg.Memory.SummaryThreshold)
	}
	convStore = memory.NewDualWriteConversationStore(convStore, sessionStateStore)

	if mc, ok := b.apiAgent.(coreagent.MemoryConfigurable); ok {
		mc.SetMaxHistoryMessages(b.cfg.Memory.MaxHistoryMessages)
	}

	memoryBridge := memory.NewMarkdownMemoryBridge(markdownStore, nil)

	if b.cfg.Vector.Enabled && b.cfg.Vector.DatabaseURL != "" {
		b.initVectorSearch(markdownStore, memoryBridge, summarizingStore)
	} else {
		slog.Info("vector search disabled (keyword matching only)")
	}

	// Create ConversationManager to consolidate history + memory retrieval.
	conversationMgr := memory.NewConversationManager(convStore, memoryBridge, b.cfg.Memory.MaxHistoryMessages)

	if mc, ok := b.apiAgent.(coreagent.MemoryConfigurable); ok {
		mc.SetMemoryManager(memoryBridge)
	}
	if cc, ok := b.apiAgent.(agentpkg.ConversationConfigurable); ok {
		cc.SetConversationManager(conversationMgr)
	}
	slog.Info("conversation manager initialized")

	b.markdownStore = markdownStore
	b.sessionStore = sessionStateStore
	return b
}

// initVectorSearch initializes optional PostgreSQL + pgvector for semantic search.
func (b *ServerBuilder) initVectorSearch(markdownStore *memory.MarkdownMemoryStore, memoryBridge *memory.MarkdownMemoryBridge, summarizingStore *memory.SummarizingConversationStore) {
	pgPool, err := pgxpool.New(context.Background(), b.cfg.Vector.DatabaseURL)
	if err != nil {
		b.fail("failed to parse vector database URL", err)
		return
	}
	if err := pgPool.Ping(context.Background()); err != nil {
		pgPool.Close()
		b.fail("failed to connect to vector database", err)
		return
	}

	vectorStore := memory.NewPgVectorStore(pgPool, b.cfg.Vector.Dimensions)
	if err := vectorStore.Migrate(context.Background()); err != nil {
		b.fail("failed to migrate vector store", err)
		return
	}

	embeddingSvc := memory.NewEmbeddingService(b.modelRouter, b.cfg.Vector.Model, b.cfg.Vector.Dimensions)
	memoryBridge.SetRetrievalStrategy(memory.NewVectorFirstStrategy(markdownStore, vectorStore, embeddingSvc))

	indexer := memory.NewAsyncEmbeddingIndexer(embeddingSvc, vectorStore)
	if summarizingStore != nil {
		summarizingStore.SetIndexer(indexer)
	}
	slog.Info("vector search enabled", "model", b.cfg.Vector.Model, "dimensions", b.cfg.Vector.Dimensions)
}
