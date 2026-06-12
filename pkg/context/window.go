package contextmgr

import "klyra/pkg/llm"

type Window struct {
	maxMessages int
	maxTokens   int
	messages    []llm.Message
	tokenScale  float64 // calibration factor derived from actual API usage
}

func NewWindow(maxMessages int) *Window {
	if maxMessages < 4 {
		maxMessages = 4
	}
	return &Window{maxMessages: maxMessages}
}

func NewBudgetedWindow(maxMessages int, maxTokens int) *Window {
	window := NewWindow(maxMessages)
	window.maxTokens = maxTokens
	return window
}

func (w *Window) Add(message llm.Message) {
	w.messages = append(w.messages, message)
	w.trim()
}

// CalibrateFrom updates the internal token scale factor from an actual API
// response token count. Call this after each LLM response with
// resp.Usage.InputTokens. Uses exponential moving average (α=0.3) so the
// estimate converges quickly without overreacting to a single outlier.
func (w *Window) CalibrateFrom(actualTokens int) {
	if actualTokens <= 0 || len(w.messages) == 0 {
		return
	}
	estimated := EstimateMessagesTokens(w.messages)
	if estimated <= 0 {
		return
	}
	scale := float64(actualTokens) / float64(estimated)
	if w.tokenScale == 0 {
		w.tokenScale = scale
	} else {
		w.tokenScale = 0.7*w.tokenScale + 0.3*scale
	}
}

// adjustedMaxTokens returns maxTokens corrected by the calibration factor.
// When actual usage exceeds our estimate (scale > 1), the budget shrinks so
// PackMessages evicts old messages sooner, preventing context window overflow.
// Clamped to at least 25% of maxTokens to avoid over-aggressive eviction.
func (w *Window) adjustedMaxTokens() int {
	if w.tokenScale <= 0 || w.maxTokens <= 0 {
		return w.maxTokens
	}
	adjusted := int(float64(w.maxTokens) / w.tokenScale)
	min := w.maxTokens / 4
	if adjusted < min {
		return min
	}
	return adjusted
}

func (w *Window) Messages() []llm.Message {
	out := make([]llm.Message, len(w.messages))
	copy(out, w.messages)
	if w.maxTokens > 0 {
		out, _ = PackMessages(out, w.adjustedMaxTokens(), w.maxMessages)
	}
	return out
}

func (w *Window) trim() {
	if len(w.messages) <= w.maxMessages {
		return
	}

	system := make([]llm.Message, 0, 1)
	if len(w.messages) > 0 && w.messages[0].Role == llm.RoleSystem {
		system = append(system, w.messages[0])
	}

	tailSize := w.maxMessages - len(system)
	tail := w.messages[len(w.messages)-tailSize:]
	w.messages = append(system, tail...)
}
