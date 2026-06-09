package harness

import (
	"context"
	"fmt"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// CompactionTrigger defines when context compaction should occur.
type CompactionTrigger struct {
	MaxTurns  int // compact when turn count exceeds this
	MaxTokens int // compact when estimated tokens exceed this (rough: 4 chars ≈ 1 token)
}

// DefaultCompactionTrigger returns standard compaction thresholds.
func DefaultCompactionTrigger() CompactionTrigger {
	return CompactionTrigger{
		MaxTurns:  4,
		MaxTokens: 8000,
	}
}

// ShouldCompact returns true if compaction should be triggered.
func (ct CompactionTrigger) ShouldCompact(turnCount int, estimatedTokens int) bool {
	if ct.MaxTurns > 0 && turnCount >= ct.MaxTurns {
		return true
	}
	if ct.MaxTokens > 0 && estimatedTokens >= ct.MaxTokens {
		return true
	}
	return false
}

// CompactionStrategy determines how context is compacted.
type CompactionStrategy interface {
	ShouldCompact(turnCount int, estimatedTokens int) bool
	Compact(ctx context.Context, turns []TurnResult) ([]TurnResult, error)
}

// ThresholdCompactionStrategy compacts based on turn count and token estimates.
// Keeps first turn (context) + last N turns (recency).
type ThresholdCompactionStrategy struct {
	trigger         CompactionTrigger
	maxRetainedTurns int
}

// NewThresholdCompactionStrategy creates a threshold-based strategy.
func NewThresholdCompactionStrategy(trigger CompactionTrigger, maxRetainedTurns int) *ThresholdCompactionStrategy {
	return &ThresholdCompactionStrategy{
		trigger:         trigger,
		maxRetainedTurns: maxRetainedTurns,
	}
}

// ShouldCompact checks if compaction is needed based on thresholds.
func (s *ThresholdCompactionStrategy) ShouldCompact(turnCount int, estimatedTokens int) bool {
	return s.trigger.ShouldCompact(turnCount, estimatedTokens)
}

// Compact retains first turn + last N turns, dropping middle turns.
func (s *ThresholdCompactionStrategy) Compact(ctx context.Context, turns []TurnResult) ([]TurnResult, error) {
	if len(turns) <= 2 || s.maxRetainedTurns <= 0 {
		return turns, nil
	}

	if s.maxRetainedTurns >= len(turns) {
		return turns, nil
	}

	// Keep first turn (system context) + last N-1 turns (recency)
	result := make([]TurnResult, 0, s.maxRetainedTurns)
	result = append(result, turns[0])
	start := len(turns) - s.maxRetainedTurns + 1
	if start < 1 {
		start = 1
	}
	result = append(result, turns[start:]...)
	return result, nil
}

// EstimateTokens provides a rough token estimate from turn results.
func EstimateTokens(turns []TurnResult) int {
	totalChars := 0
	for _, t := range turns {
		totalChars += len(t.PlanText)
		for _, obs := range t.Observations {
			totalChars += len(obs)
		}
	}
	// Rough: 4 characters per token
	return totalChars / 4
}

// ContextCompactorAdapter adapts a CompactionStrategy to the ContextCompactor interface.
type ContextCompactorAdapter struct {
	strategy CompactionStrategy
}

// NewContextCompactorAdapter creates an adapter.
func NewContextCompactorAdapter(strategy CompactionStrategy) *ContextCompactorAdapter {
	return &ContextCompactorAdapter{strategy: strategy}
}

// Compact implements ContextCompactor.
func (a *ContextCompactorAdapter) Compact(ctx context.Context, turnResults []TurnResult) ([]TurnResult, error) {
	estTokens := EstimateTokens(turnResults)
	if !a.strategy.ShouldCompact(len(turnResults), estTokens) {
		return turnResults, nil
	}
	result, err := a.strategy.Compact(ctx, turnResults)
	if err != nil {
		return nil, fmt.Errorf("context compaction failed: %w", err)
	}
	return result, nil
}

// EstimateMessageTokens estimates token count from message content using chars/4 heuristic.
func EstimateMessageTokens(msgs []aitypes.Message) int {
	return estimateMessageChars(msgs) / 4
}

func estimateMessageChars(msgs []aitypes.Message) int {
	totalChars := 0
	for _, m := range msgs {
		for _, c := range m.Contents {
			if c.Type == "text" && c.Text != "" {
				totalChars += len(c.Text)
			}
		}
	}
	return totalChars
}

// TruncateHistoryByToken drops oldest messages to fit within maxTokens.
// Always keeps at least 1 message (the most recent) if input is non-empty.
// Returns nil for nil input.
func TruncateHistoryByToken(history []aitypes.Message, maxTokens int) []aitypes.Message {
	if history == nil {
		return nil
	}
	if maxTokens <= 0 {
		return nil
	}
	maxChars := maxTokens * 4
	if estimateMessageChars(history) <= maxChars {
		return history
	}
	// Walk from end (most recent), accumulate chars, keep tail within budget
	var kept []aitypes.Message
	accumulated := 0
	for i := len(history) - 1; i >= 0; i-- {
		msgChars := estimateMessageChars(history[i : i+1])
		if accumulated+msgChars > maxChars && len(kept) > 0 {
			break
		}
		accumulated += msgChars
		kept = append(kept, history[i])
	}
	// Reverse to maintain original order
	for l, r := 0, len(kept)-1; l < r; l, r = l+1, r-1 {
		kept[l], kept[r] = kept[r], kept[l]
	}
	return kept
}
