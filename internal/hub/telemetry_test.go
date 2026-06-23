package hub

import (
	"testing"

	"github.com/shleesauce/lattice/internal/transcript"
)

// modelContextWindow returns 1M for the [1m] variants and 200K otherwise.
func TestModelContextWindow(t *testing.T) {
	cases := map[string]int{
		"claude-opus-4-8":     200_000,
		"claude-opus-4-8[1m]": 1_000_000,
		"claude-sonnet-4-6":   200_000,
		"":                    200_000,
		"unknown-model":       200_000,
	}
	for model, want := range cases {
		if got := modelContextWindow(model); got != want {
			t.Errorf("modelContextWindow(%q)=%d want %d", model, got, want)
		}
	}
}

// contextPct tracks the working set against the window and clamps to 0..100.
func TestContextPct(t *testing.T) {
	// 100K cache-read + 10K output against a 200K window ≈ 55%.
	m := transcript.Meta{CacheReadTokens: 100_000, OutputTokens: 10_000}
	pct := contextPct(m, "claude-opus-4-8")
	if pct < 54 || pct > 56 {
		t.Errorf("contextPct standard window: got %.1f want ~55", pct)
	}
	// Same footprint on a 1M window is far smaller.
	pct1m := contextPct(m, "claude-opus-4-8[1m]")
	if pct1m >= pct {
		t.Errorf("1M window should yield lower context%%: 1m=%.2f std=%.2f", pct1m, pct)
	}
	// Over-full clamps to 100.
	full := transcript.Meta{CacheReadTokens: 500_000}
	if got := contextPct(full, "claude-opus-4-8"); got != 100 {
		t.Errorf("over-full should clamp to 100, got %.1f", got)
	}
	// No cache reads yet → falls back to raw input.
	fresh := transcript.Meta{InputTokens: 20_000}
	if got := contextPct(fresh, "claude-opus-4-8"); got <= 0 {
		t.Errorf("fresh session should use input footprint, got %.1f", got)
	}
}

// estimateCostUSD is monotonic in tokens and non-negative.
func TestEstimateCostUSD(t *testing.T) {
	if c := estimateCostUSD(transcript.Meta{}); c != 0 {
		t.Errorf("empty meta should cost 0, got %f", c)
	}
	small := estimateCostUSD(transcript.Meta{InputTokens: 1000, OutputTokens: 500})
	big := estimateCostUSD(transcript.Meta{InputTokens: 100_000, OutputTokens: 50_000})
	if big <= small {
		t.Errorf("more tokens should cost more: small=%f big=%f", small, big)
	}
}
