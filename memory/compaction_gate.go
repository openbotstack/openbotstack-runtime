package memory

import (
	"context"
	"log/slog"
	"sync"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// compactionGate encapsulates the compaction triggering, planning, and execution
// logic extracted from SummarizingConversationStore. It handles:
//   - Per-session compaction throttle + dedup
//   - Compaction plan building from zoned history
//   - Compaction plan execution via Compactor
//   - Result application back to zoned history
//
// All fields are private; the parent SummarizingConversationStore orchestrates
// when to call trigger/run.
type compactionGate struct {
	compactor      Compactor
	zonedStore     ZonedStore
	maxTokens      int
	pending        map[string]struct{}
	counts         map[string]int
	mu             sync.Mutex
}

func newCompactionGate(compactor Compactor, zonedStore ZonedStore) *compactionGate {
	return &compactionGate{
		compactor:  compactor,
		zonedStore: zonedStore,
		maxTokens:  16000,
		pending:    make(map[string]struct{}),
		counts:     make(map[string]int),
	}
}

// initialized reports whether the session already has compaction state.
func (g *compactionGate) initialized(sessionID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.counts[sessionID]
	return ok
}

// initWithCount sets the initial count for a session (from persistent store).
func (g *compactionGate) initWithCount(sessionID string, count int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.counts[sessionID]; !ok {
		g.counts[sessionID] = count
	}
}

// clearSession removes all compaction state for a session.
func (g *compactionGate) clearSession(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, sessionID)
	delete(g.counts, sessionID)
}

// shouldTrigger returns true when a compaction goroutine should launch.
// Enforces: contentLen >= 20, count%5==0 throttle, per-session dedup.
func (g *compactionGate) shouldTrigger(sessionID string, contentLen int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.pending[sessionID]; ok {
		g.counts[sessionID]++
		return false
	}
	g.counts[sessionID]++
	if contentLen < 20 {
		return false
	}
	if g.counts[sessionID]%5 != 0 {
		return false
	}
	g.pending[sessionID] = struct{}{}
	return true
}

func (g *compactionGate) clearPending(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, sessionID)
}

// run executes the compaction cycle: token check → plan → compact → write.
func (g *compactionGate) run(ctx context.Context, tenantID, userID, sessionID string) {
	zoned, err := g.zonedStore.GetZonedHistory(ctx, tenantID, userID, sessionID)
	if err != nil || len(zoned) == 0 {
		return
	}

	totalTokens := EstimateZonedTokens(zoned)
	if totalTokens <= g.maxTokens {
		return
	}

	plan := g.buildCompactionPlan(zoned)
	result, err := g.compactor.Compact(ctx, plan)
	if err != nil {
		slog.Warn("compaction failed", "session_id", sessionID, "error", err)
		return
	}

	updated := g.applyCompactionResult(plan, result)
	if err := g.zonedStore.WriteZonedHistory(ctx, tenantID, userID, sessionID, updated); err != nil {
		slog.Warn("compaction write failed", "session_id", sessionID, "error", err)
	}
}

// buildCompactionPlan partitions zoned messages into compression/archive targets.
// Only the oldest half of full-zone messages and compressed turns are selected.
func (g *compactionGate) buildCompactionPlan(zoned []ZonedMessage) CompactionPlan {
	var plan CompactionPlan
	var fullMsgs []aitypes.Message
	var compressedTurns []TurnSummary

	for _, zm := range zoned {
		switch zm.Zone {
		case ZoneArchive:
			plan.ArchiveSummary = zm.ArchiveSummary
		case ZoneCompressed:
			if zm.TurnSummary != nil {
				compressedTurns = append(compressedTurns, *zm.TurnSummary)
			}
		case ZoneFull:
			if zm.Message != nil {
				fullMsgs = append(fullMsgs, *zm.Message)
			}
		}
	}

	if len(fullMsgs) > 5 {
		compressCount := len(fullMsgs) / 2
		plan.MessagesToCompress = fullMsgs[:compressCount]
		plan.fullMsgsToKeep = fullMsgs[compressCount:]
	} else {
		plan.fullMsgsToKeep = fullMsgs
	}

	if len(compressedTurns) > 5 {
		archiveCount := len(compressedTurns) / 2
		plan.TurnsToArchive = compressedTurns[:archiveCount]
		plan.compressedToKeep = compressedTurns[archiveCount:]
	} else {
		plan.compressedToKeep = compressedTurns
	}

	return plan
}

// applyCompactionResult rebuilds the zoned message list after compaction.
func (g *compactionGate) applyCompactionResult(plan CompactionPlan, result *CompactionResult) []ZonedMessage {
	var updated []ZonedMessage

	if result.UpdatedArchive != "" {
		updated = append(updated, ZonedMessage{Zone: ZoneArchive, ArchiveSummary: result.UpdatedArchive})
	} else if plan.ArchiveSummary != "" {
		updated = append(updated, ZonedMessage{Zone: ZoneArchive, ArchiveSummary: plan.ArchiveSummary})
	}

	for _, ts := range result.NewTurnSummaries {
		tsCopy := ts
		updated = append(updated, ZonedMessage{Zone: ZoneCompressed, TurnSummary: &tsCopy})
	}
	for _, ts := range plan.compressedToKeep {
		tsCopy := ts
		updated = append(updated, ZonedMessage{Zone: ZoneCompressed, TurnSummary: &tsCopy})
	}

	for i := range plan.fullMsgsToKeep {
		updated = append(updated, ZonedMessage{Zone: ZoneFull, Message: &plan.fullMsgsToKeep[i]})
	}

	return updated
}
