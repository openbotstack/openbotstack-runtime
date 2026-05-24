package agent

import (
	"strings"
	"sync"
)

// KeywordWorkflowResolver matches user messages to registered workflows
// using keyword-based substring matching. All keywords in a pattern must
// appear in the message (AND logic). First matching pattern wins.
type KeywordWorkflowResolver struct {
	mu       sync.RWMutex
	patterns []workflowPattern
}

type workflowPattern struct {
	workflow Workflow
	keywords []string
	input    map[string]any
}

// NewKeywordWorkflowResolver creates a resolver with no registered patterns.
func NewKeywordWorkflowResolver() *KeywordWorkflowResolver {
	return &KeywordWorkflowResolver{}
}

// Register adds a workflow with keyword patterns and default input.
// Keywords are matched case-insensitively as substrings (AND logic).
// Registration order determines priority: first match wins.
// Panics if keywords is empty or contains empty/whitespace-only strings.
func (r *KeywordWorkflowResolver) Register(workflow Workflow, keywords []string, defaultInput map[string]any) {
	if len(keywords) == 0 {
		panic("keyword_resolver: keywords must not be empty")
	}
	for _, kw := range keywords {
		if strings.TrimSpace(kw) == "" {
			panic("keyword_resolver: keyword must not be empty or whitespace-only")
		}
	}

	input := defaultInput
	if input != nil {
		input = copyMap(defaultInput)
	}
	r.mu.Lock()
	r.patterns = append(r.patterns, workflowPattern{
		workflow: workflow,
		keywords: keywords,
		input:    input,
	})
	r.mu.Unlock()
}

// Resolve matches a user message against registered keyword patterns.
// Returns (nil, nil, nil) when no pattern matches.
func (r *KeywordWorkflowResolver) Resolve(message string) (Workflow, map[string]any, error) {
	if message == "" {
		return nil, nil, nil
	}

	msgLower := strings.ToLower(message)

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.patterns {
		if allKeywordsMatch(msgLower, p.keywords) {
			var input map[string]any
			if p.input != nil {
				input = copyMap(p.input)
			}
			return p.workflow, input, nil
		}
	}

	return nil, nil, nil
}

func allKeywordsMatch(msgLower string, keywords []string) bool {
	for _, kw := range keywords {
		if !strings.Contains(msgLower, strings.ToLower(kw)) {
			return false
		}
	}
	return true
}

func copyMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
