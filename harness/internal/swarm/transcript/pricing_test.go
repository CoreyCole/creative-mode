package transcript

import (
	"math"
	"testing"
)

func TestEstimateCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		model         string
		input         int64
		output        int64
		cacheRead     int64
		cacheCreation int64
		wantMin       float64
		wantMax       float64
	}{
		{
			name:          "sonnet basic",
			model:         "claude-sonnet-4-20250514",
			input:         1_000_000,
			output:        100_000,
			cacheRead:     0,
			cacheCreation: 0,
			wantMin:       4.4, // $3 input + $1.5 output
			wantMax:       4.6,
		},
		{
			name:          "opus with cache",
			model:         "claude-opus-4-20250514",
			input:         500_000,
			output:        50_000,
			cacheRead:     1_000_000,
			cacheCreation: 200_000,
			wantMin:       12.0, // $7.5 + $3.75 + $1.50 + $0.75
			wantMax:       14.0,
		},
		{
			name:          "haiku cheap",
			model:         "claude-haiku-4-5-20250514",
			input:         100_000,
			output:        10_000,
			cacheRead:     0,
			cacheCreation: 0,
			wantMin:       0.10,
			wantMax:       0.15,
		},
		{
			name:          "unknown model defaults to sonnet",
			model:         "unknown-model-2025",
			input:         1_000_000,
			output:        0,
			cacheRead:     0,
			cacheCreation: 0,
			wantMin:       2.9,
			wantMax:       3.1,
		},
		{
			name:          "zero tokens",
			model:         "claude-sonnet-4-20250514",
			input:         0,
			output:        0,
			cacheRead:     0,
			cacheCreation: 0,
			wantMin:       0,
			wantMax:       0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := EstimateCost(
				tt.model,
				tt.input,
				tt.output,
				tt.cacheRead,
				tt.cacheCreation,
			)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("EstimateCost(%q, %d, %d, %d, %d) = %f, want [%f, %f]",
					tt.model, tt.input, tt.output, tt.cacheRead, tt.cacheCreation,
					got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestResolvePricing_CaseInsensitive(t *testing.T) {
	t.Parallel()

	p1 := resolvePricing("Claude-OPUS-4-20250514")
	p2 := resolvePricing("claude-opus-4-20250514")

	if math.Abs(p1.Input-p2.Input) > 0.001 {
		t.Error("pricing should be case-insensitive")
	}
}
