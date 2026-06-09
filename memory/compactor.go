package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-runtime/harness"
)

const compactionTimeout = 15 * time.Second

// Compactor is the interface for session compaction operations.
type Compactor interface {
	Compact(ctx context.Context, plan CompactionPlan) (*CompactionResult, error)
}

// turnSummaryJSON is the JSON structure returned by LLM for turn compression.
type turnSummaryJSON struct {
	Topic     string   `json:"topic"`
	Summary   string   `json:"summary"`
	Decisions []string `json:"decisions"`
	Facts     []string `json:"facts"`
}

// SessionCompactor performs progressive compression using an LLM.
type SessionCompactor struct {
	router providers.ModelRouter
}

// NewSessionCompactor creates a compactor backed by the given model router.
func NewSessionCompactor(router providers.ModelRouter) *SessionCompactor {
	return &SessionCompactor{router: router}
}

// Compact executes one round of progressive compression.
// 1. MessagesToCompress → TurnSummaries (Zone 1 → Zone 2)
// 2. TurnsToArchive → merged ArchiveSummary (Zone 2 → Zone 3)
func (c *SessionCompactor) Compact(ctx context.Context, plan CompactionPlan) (*CompactionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compactor: context cancelled: %w", err)
	}

	result := &CompactionResult{}

	if len(plan.MessagesToCompress) == 0 && len(plan.TurnsToArchive) == 0 {
		return result, nil
	}

	// Step 1: Compress messages into turn summaries
	if len(plan.MessagesToCompress) > 0 {
		originalTokens := harness.EstimateMessageTokens(plan.MessagesToCompress)
		summaries, err := c.compressMessages(ctx, plan.MessagesToCompress)
		if err != nil {
			return nil, fmt.Errorf("compactor: compress messages: %w", err)
		}
		result.NewTurnSummaries = summaries
		compressedTokens := 0
		for _, ts := range summaries {
			compressedTokens += ts.EstimateTokens()
		}
		saved := originalTokens - compressedTokens
		if saved > 0 {
			result.TokensSaved += saved
		}
	}

	// Step 2: Archive turn summaries
	if len(plan.TurnsToArchive) > 0 {
		archive, err := c.archiveTurns(ctx, plan.TurnsToArchive, plan.ArchiveSummary)
		if err != nil {
			return nil, fmt.Errorf("compactor: archive turns: %w", err)
		}
		result.UpdatedArchive = archive
	}

	return result, nil
}

func (c *SessionCompactor) compressMessages(ctx context.Context, msgs []aitypes.Message) ([]TurnSummary, error) {
	schema := &aitypes.JSONSchema{
		Type: "object",
		Properties: map[string]*aitypes.JSONSchema{
			"summaries": {
				Type: "array",
				Items: &aitypes.JSONSchema{
					Type: "object",
					Properties: map[string]*aitypes.JSONSchema{
						"topic":     {Type: "string"},
						"summary":   {Type: "string"},
						"decisions": {Type: "array", Items: &aitypes.JSONSchema{Type: "string"}},
						"facts":     {Type: "array", Items: &aitypes.JSONSchema{Type: "string"}},
					},
					Required: []string{"topic", "summary", "decisions", "facts"},
					AdditionalProperties: func(b bool) *bool { return &b }(false),
				},
			},
		},
		Required: []string{"summaries"},
		AdditionalProperties: func(b bool) *bool { return &b }(false),
	}

	prompt := buildCompressionPrompt(msgs)
	resp, err := c.callLLM(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		Summaries []turnSummaryJSON `json:"summaries"`
	}
	if err := json.Unmarshal([]byte(resp), &wrapper); err != nil {
		return nil, fmt.Errorf("compactor: invalid LLM response: %w", err)
	}
	summaries := wrapper.Summaries

	result := make([]TurnSummary, len(summaries))
	for i, s := range summaries {
		result[i] = TurnSummary{
			Timestamp: time.Now().Format(time.RFC3339),
			Topic:     s.Topic,
			Summary:   s.Summary,
			Decisions: s.Decisions,
			Facts:     s.Facts,
		}
	}
	return result, nil
}

func (c *SessionCompactor) archiveTurns(ctx context.Context, turns []TurnSummary, existingArchive string) (string, error) {
	prompt := buildArchivePrompt(turns, existingArchive)
	resp, err := c.callLLM(ctx, prompt, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp), nil
}

func (c *SessionCompactor) callLLM(ctx context.Context, prompt string, schema *aitypes.JSONSchema) (string, error) {
	caps := []aitypes.CapabilityType{aitypes.CapTextGeneration}
	if schema != nil {
		caps = append(caps, aitypes.CapJSONMode)
	}
	prov, err := c.router.Route(caps, aitypes.ModelConstraints{})
	if err != nil {
		return "", fmt.Errorf("routing failed: %w", err)
	}

	req := aitypes.GenerateRequest{
		Messages: []aitypes.Message{
			aitypes.NewTextMessage("user", prompt),
		},
		MaxTokens:   2048,
		Temperature: 0.3,
	}
	
	if schema != nil {
		req.JSONSchema = schema
	}

	resp, err := prov.Generate(ctx, req)
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	return resp.Content, nil
}

func buildCompressionPrompt(msgs []aitypes.Message) string {
	var sb strings.Builder
	sb.WriteString("Compress the following conversation into structured turn summaries. ")
	sb.WriteString("Return a JSON object with a single field 'summaries' which is an array of objects with fields: topic, summary, decisions, facts.\n")
	sb.WriteString("Each message pair (user+assistant) should produce one summary object.\n")
	sb.WriteString("Keep summaries concise. Only include non-trivial decisions and facts.\n\n")
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(aitypes.FlattenToText(m.Contents))
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildArchivePrompt(turns []TurnSummary, existingArchive string) string {
	var sb strings.Builder
	sb.WriteString("Merge the following turn summaries into a single concise session summary. ")
	sb.WriteString("Preserve key decisions, facts, and important context. Write in third person.\n\n")

	if existingArchive != "" {
		sb.WriteString("Existing archive:\n")
		sb.WriteString(existingArchive)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Turn summaries to merge:\n")
	for _, t := range turns {
		sb.WriteString(fmt.Sprintf("- Topic: %s\n  Summary: %s\n", t.Topic, t.Summary))
		for _, d := range t.Decisions {
			sb.WriteString(fmt.Sprintf("  Decision: %s\n", d))
		}
		for _, f := range t.Facts {
			sb.WriteString(fmt.Sprintf("  Fact: %s\n", f))
		}
	}
	return sb.String()
}
