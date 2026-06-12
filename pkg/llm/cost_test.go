package llm

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestEstimateCostKnownModels(t *testing.T) {
	cases := []struct {
		name  string
		model string
		usage Usage
		want  float64
	}{
		{
			name:  "gpt-4o input and output",
			model: "gpt-4o",
			usage: Usage{InputTokens: 1000, OutputTokens: 1000, TotalTokens: 2000},
			// in 2.50/1M, out 10.00/1M -> 0.0025 + 0.01
			want: 0.0125,
		},
		{
			name:  "claude-sonnet-4 output only",
			model: "claude-sonnet-4-6",
			usage: Usage{OutputTokens: 2000, TotalTokens: 2000},
			// out 15.00/1M -> 0.03
			want: 0.03,
		},
		{
			name:  "gemini-2.5-flash mixed",
			model: "gemini-2.5-flash",
			usage: Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000},
			// in 0.15, out 0.60
			want: 0.75,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EstimateCost(tc.model, tc.usage)
			if !almostEqual(got, tc.want) {
				t.Fatalf("EstimateCost(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestEstimateCostCachedDiscount(t *testing.T) {
	// gpt-4o: input 2.50/1M, cached portion billed at 10% = 0.25/1M.
	// 1000 input of which 1000 cached, no output -> fresh=0, cached=1000*0.25/1e6.
	got := EstimateCost("gpt-4o", Usage{InputTokens: 1000, CachedTokens: 1000})
	want := 1000.0 * (2.50 * 0.1) / 1e6
	if !almostEqual(got, want) {
		t.Fatalf("cached cost = %v, want %v", got, want)
	}

	// Cached tokens must cost strictly less than the same volume of fresh input.
	fresh := EstimateCost("gpt-4o", Usage{InputTokens: 1000})
	if got >= fresh {
		t.Fatalf("cached cost %v should be cheaper than fresh cost %v", got, fresh)
	}
}

func TestEstimateCostUnknownModelIsZero(t *testing.T) {
	if got := EstimateCost("some-unlisted-model", Usage{InputTokens: 1000, OutputTokens: 1000}); got != 0 {
		t.Fatalf("unknown model should cost 0, got %v", got)
	}
	if got := EstimateCost("", Usage{InputTokens: 1000}); got != 0 {
		t.Fatalf("empty model should cost 0, got %v", got)
	}
}

func TestEstimateCostIsCaseInsensitive(t *testing.T) {
	lower := EstimateCost("gpt-4o", Usage{InputTokens: 1000, OutputTokens: 1000})
	upper := EstimateCost("GPT-4o", Usage{InputTokens: 1000, OutputTokens: 1000})
	if !almostEqual(lower, upper) {
		t.Fatalf("case should not change cost: %v vs %v", lower, upper)
	}
}
