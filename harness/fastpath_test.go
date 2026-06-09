package harness

import "testing"

func TestIsSimpleRespondRequest(t *testing.T) {
	tests := []struct {
		msg      string
		expected bool
	}{
		{"hello", true},
		{"What is the weather today?", true},
		{"Tell me about Go", true},
		{shortMsg(99), true},   // just under limit
		{shortMsg(100), true},  // exactly at limit
		{shortMsg(101), false}, // just over limit
		{"use the search tool to find X", false},
		{"call the API", false},
		{"execute the script", false},
		{"run the analysis tool", false},
		{"fetch data from URL", false},
		{"use mcp.database query", false},
		{"try builtin.now", false},
		{"please use skill summarize", false},
		{"can you search for that", false},
		{"read file /tmp/data", false},
		{"write file output", false},
		{"", true}, // empty is trivially simple
	}

	for _, tt := range tests {
		got := isSimpleRespondRequest(tt.msg)
		if got != tt.expected {
			t.Errorf("isSimpleRespondRequest(%q) = %v, want %v", truncMsg(tt.msg, 40), got, tt.expected)
		}
	}
}

func shortMsg(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func truncMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
