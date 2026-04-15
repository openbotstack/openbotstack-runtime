package memory

import (
	"math"
	"testing"
)

func TestFormatVector_Empty(t *testing.T) {
	got := formatVector(nil)
	if got != "[]" {
		t.Errorf("expected '[]', got %q", got)
	}
}

func TestFormatVector_SingleElement(t *testing.T) {
	got := formatVector([]float32{1.5})
	if got != "[1.5]" {
		t.Errorf("expected '[1.5]', got %q", got)
	}
}

func TestFormatVector_MultipleElements(t *testing.T) {
	got := formatVector([]float32{0.1, -0.2, 0.3})
	if got != "[0.1,-0.2,0.3]" {
		t.Errorf("expected '[0.1,-0.2,0.3]', got %q", got)
	}
}

func TestFormatVector_Float32Precision(t *testing.T) {
	// This is the critical test from the audit:
	// %f would lose precision for float32 values like 0.12345679
	v := float32(0.12345679)
	got := formatVector([]float32{v})

	// Should preserve full float32 precision (not truncate to 6 decimal places)
	if got == "[0.123457]" {
		t.Errorf("formatVector truncated float32 precision: got %q", got)
	}
	// The value should be parseable back to the same float32
}

func TestFormatVector_SmallValues(t *testing.T) {
	// Test very small values near float32 precision limits
	got := formatVector([]float32{1.0000001})
	if got == "[1.000000]" {
		t.Errorf("formatVector lost precision for 1.0000001: got %q", got)
	}
}

func TestFormatVector_LargeValues(t *testing.T) {
	got := formatVector([]float32{123.456, -789.012})
	if got != "[123.456,-789.012]" {
		t.Errorf("expected '[123.456,-789.012]', got %q", got)
	}
}

func TestNewPgVectorStore(t *testing.T) {
	store := NewPgVectorStore(nil, 512)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.dimensions != 512 {
		t.Errorf("expected dimensions 512, got %d", store.dimensions)
	}
}

func TestPgVectorStore_Close(t *testing.T) {
	// Close should be a no-op (caller owns the pool)
	store := NewPgVectorStore(nil, 512)
	if err := store.Close(); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestSearchOptions_Defaults(t *testing.T) {
	opts := SearchOptions{TenantID: "t1"}
	if opts.Limit != 0 {
		t.Errorf("expected default limit 0, got %d", opts.Limit)
	}
	if opts.UserID != "" {
		t.Errorf("expected empty UserID, got %q", opts.UserID)
	}
}

func TestVectorDocument_Fields(t *testing.T) {
	doc := VectorDocument{
		ID:        "test-id",
		Content:   "hello world",
		Embedding: []float32{0.1, 0.2, 0.3},
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Role:      "user",
	}
	if doc.ID != "test-id" {
		t.Errorf("unexpected ID: %q", doc.ID)
	}
	if len(doc.Embedding) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(doc.Embedding))
	}
}

func TestSearchResult_Score(t *testing.T) {
	r := SearchResult{
		VectorDocument: VectorDocument{ID: "1", Content: "test"},
		Score:         0.95,
	}
	if r.Score < 0.9 || r.Score > 1.0 {
		t.Errorf("expected score near 0.95, got %f", r.Score)
	}
}

func TestDeleteFilter(t *testing.T) {
	f := DeleteFilter{
		TenantID:  "t1",
		SessionID: "s1",
	}
	if f.TenantID != "t1" {
		t.Errorf("expected TenantID t1, got %q", f.TenantID)
	}
}

func TestFormatVector_PreservesFloat32Range(t *testing.T) {
	// Test edge cases of float32 range
	tests := []struct {
		name string
		val  float32
	}{
		{"zero", 0},
		{"negative", -1.5},
		{"small_positive", 0.0001},
		{"large", 1000.5},
		{"max_float32", math.MaxFloat32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVector([]float32{tt.val})
			if len(got) < 3 { // at least "[X]"
				t.Errorf("formatVector(%f) produced too short: %q", tt.val, got)
			}
		})
	}
}
