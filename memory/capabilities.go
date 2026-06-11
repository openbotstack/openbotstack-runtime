package memory

import (
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

// ResolvedCapabilities holds optional interface capabilities resolved from a
// ConversationStore at construction time. This replaces the repeated
// type-assertion blocks that were duplicated across ConversationManager,
// DualWriteConversationStore, and SummarizingConversationStore.
type ResolvedCapabilities struct {
	ZonedProvider   ZonedHistoryProvider
	SummaryMeta     SummaryMetaProvider
	MessageCounter  MessageCountProvider
	ZonedStore      ZonedStore
}

// ResolveCapabilities sniffs the inner store for optional interface capabilities.
// Call once at construction time; store the result in the decorator's fields.
func ResolveCapabilities(inner coreagent.ConversationStore) ResolvedCapabilities {
	var caps ResolvedCapabilities

	if zoned, ok := inner.(ZonedHistoryProvider); ok {
		caps.ZonedProvider = zoned
	}
	if meta, ok := inner.(SummaryMetaProvider); ok {
		caps.SummaryMeta = meta
	}
	if counter, ok := inner.(MessageCountProvider); ok {
		caps.MessageCounter = counter
	}
	if zs, ok := inner.(ZonedStore); ok {
		caps.ZonedStore = zs
	}

	return caps
}
