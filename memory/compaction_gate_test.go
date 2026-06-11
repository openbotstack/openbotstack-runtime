package memory

import (
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

func TestCompactionGate_ShouldTrigger_FirstMessageSkips(t *testing.T) {
	g := newCompactionGate(nil, nil)
	if g.shouldTrigger("s1", 5) {
		t.Error("should not trigger on short message with count=1")
	}
}

func TestCompactionGate_ShouldTrigger_ShortMessageSkips(t *testing.T) {
	g := newCompactionGate(nil, nil)
	g.counts["s1"] = 4 // count%5==0 but content < 20
	if g.shouldTrigger("s1", 5) {
		t.Error("should not trigger when content < 20 chars")
	}
}

func TestCompactionGate_ShouldTrigger_TriggersAtThreshold(t *testing.T) {
	g := newCompactionGate(nil, nil)
	g.counts["s1"] = 4 // 5%5==0 on next increment
	if !g.shouldTrigger("s1", 100) {
		t.Error("should trigger when count%5==0 and content >= 20")
	}
}

func TestCompactionGate_ShouldTrigger_DedupWhenPending(t *testing.T) {
	g := newCompactionGate(nil, nil)
	g.pending["s1"] = struct{}{}
	if g.shouldTrigger("s1", 100) {
		t.Error("should not trigger when already pending")
	}
}

func TestCompactionGate_BuildPlan_EmptyZoned(t *testing.T) {
	g := newCompactionGate(nil, nil)
	plan := g.buildCompactionPlan(nil)
	if len(plan.MessagesToCompress) != 0 || len(plan.TurnsToArchive) != 0 {
		t.Error("nil zoned should produce empty plan")
	}
}

func TestCompactionGate_BuildPlan_FewMessagesNoCompress(t *testing.T) {
	g := newCompactionGate(nil, nil)
	zoned := []ZonedMessage{
		{Zone: ZoneFull, Message: &aitypes.Message{Role: "user"}},
		{Zone: ZoneFull, Message: &aitypes.Message{Role: "assistant"}},
	}
	plan := g.buildCompactionPlan(zoned)
	if len(plan.MessagesToCompress) != 0 {
		t.Errorf("should not compress when <=5 full messages, got %d", len(plan.MessagesToCompress))
	}
}

func TestCompactionGate_ApplyResult_PreservesArchived(t *testing.T) {
	g := newCompactionGate(nil, nil)
	plan := CompactionPlan{ArchiveSummary: "old archive"}
	result := &CompactionResult{}
	updated := g.applyCompactionResult(plan, result)
	if len(updated) != 1 || updated[0].ArchiveSummary != "old archive" {
		t.Error("should preserve archive summary when no updated archive")
	}
}

func TestCompactionGate_ClearPending_RemovesFromPending(t *testing.T) {
	g := newCompactionGate(nil, nil)
	g.pending["s1"] = struct{}{}
	g.clearPending("s1")
	if _, ok := g.pending["s1"]; ok {
		t.Error("should remove from pending")
	}
}
