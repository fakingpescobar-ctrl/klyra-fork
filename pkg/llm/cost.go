package llm

import "strings"

// EstimateCost returns the estimated cost in USD for a completed request.
// Returns 0 when the model is not in the pricing table (e.g. Ollama local models).
// Cached input tokens are billed at 10% of the normal input rate.
func EstimateCost(model string, usage Usage) float64 {
	inPer1M, outPer1M := modelPricing(model)
	if inPer1M == 0 && outPer1M == 0 {
		return 0
	}
	fresh := usage.InputTokens - usage.CachedTokens
	cost := float64(fresh)*inPer1M/1e6 +
		float64(usage.CachedTokens)*(inPer1M*0.1)/1e6 +
		float64(usage.OutputTokens)*outPer1M/1e6
	return cost
}

// modelPricing returns (inputPer1M, outputPer1M) USD for a model name.
func modelPricing(model string) (float64, float64) {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	// OpenAI
	case strings.Contains(m, "gpt-4o-mini"):
		return 0.15, 0.60
	case strings.Contains(m, "gpt-4o"):
		return 2.50, 10.00
	case strings.Contains(m, "gpt-4-turbo"):
		return 10.00, 30.00
	case strings.Contains(m, "gpt-3.5"):
		return 0.50, 1.50
	case strings.Contains(m, "o1-mini"):
		return 1.10, 4.40
	case strings.Contains(m, "o1"):
		return 15.00, 60.00
	case strings.Contains(m, "o3-mini"):
		return 1.10, 4.40
	// Anthropic — match more specific first
	case strings.Contains(m, "claude-3-5-haiku") || strings.Contains(m, "claude-haiku-4"):
		return 0.80, 4.00
	case strings.Contains(m, "claude-3-haiku"):
		return 0.25, 1.25
	case strings.Contains(m, "claude-3-5-sonnet") || strings.Contains(m, "claude-sonnet-4"):
		return 3.00, 15.00
	case strings.Contains(m, "claude-3-sonnet"):
		return 3.00, 15.00
	case strings.Contains(m, "claude-opus-4") || strings.Contains(m, "claude-3-opus"):
		return 15.00, 75.00
	// Gemini
	case strings.Contains(m, "gemini-2.5-flash"):
		return 0.15, 0.60
	case strings.Contains(m, "gemini-2.5-pro"):
		return 1.25, 10.00
	case strings.Contains(m, "gemini-2.0-flash"):
		return 0.075, 0.30
	case strings.Contains(m, "gemini-1.5-flash"):
		return 0.075, 0.30
	case strings.Contains(m, "gemini-1.5-pro"):
		return 3.50, 10.50
	default:
		return 0, 0
	}
}
