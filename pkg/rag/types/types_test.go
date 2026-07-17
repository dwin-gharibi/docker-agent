package types

import (
	"testing"
)

func TestUsage_Add(t *testing.T) {
	tests := []struct {
		name     string
		base     Usage
		other    Usage
		expected Usage
	}{
		{
			name:     "Both empty",
			base:     Usage{},
			other:    Usage{},
			expected: Usage{},
		},
		{
			name:     "Base empty, other has values",
			base:     Usage{},
			other:    Usage{TotalTokens: 10, Cost: 0.5, ModelID: "model-a"},
			expected: Usage{TotalTokens: 10, Cost: 0.5, ModelID: "model-a"},
		},
		{
			name:     "Base has values, other empty",
			base:     Usage{TotalTokens: 5, Cost: 0.2, ModelID: "model-a"},
			other:    Usage{},
			expected: Usage{TotalTokens: 5, Cost: 0.2, ModelID: "model-a"},
		},
		{
			name:     "Same model ID",
			base:     Usage{TotalTokens: 5, Cost: 0.2, ModelID: "model-a"},
			other:    Usage{TotalTokens: 10, Cost: 0.5, ModelID: "model-a"},
			expected: Usage{TotalTokens: 15, Cost: 0.7, ModelID: "model-a"},
		},
		{
			name:     "Different model IDs",
			base:     Usage{TotalTokens: 5, Cost: 0.2, ModelID: "model-a"},
			other:    Usage{TotalTokens: 10, Cost: 0.5, ModelID: "model-b"},
			expected: Usage{TotalTokens: 15, Cost: 0.7, ModelID: "model-a,model-b"},
		},
		{
			name:     "Multiple concatenations",
			base:     Usage{TotalTokens: 5, Cost: 0.2, ModelID: "model-a,model-b"},
			other:    Usage{TotalTokens: 10, Cost: 0.5, ModelID: "model-c"},
			expected: Usage{TotalTokens: 15, Cost: 0.7, ModelID: "model-a,model-b,model-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := tt.base
			u.Add(tt.other)

			if u.TotalTokens != tt.expected.TotalTokens {
				t.Errorf("expected TotalTokens %d, got %d", tt.expected.TotalTokens, u.TotalTokens)
			}

			// Float comparison with small epsilon since we're just adding constants
			if u.Cost != tt.expected.Cost {
				t.Errorf("expected Cost %f, got %f", tt.expected.Cost, u.Cost)
			}

			if u.ModelID != tt.expected.ModelID {
				t.Errorf("expected ModelID %q, got %q", tt.expected.ModelID, u.ModelID)
			}
		})
	}
}
