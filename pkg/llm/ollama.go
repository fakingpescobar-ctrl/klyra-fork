package llm

import (
	"fmt"
	"os"
	"strings"
)

// NewOllamaProvider creates a Provider backed by a local Ollama server.
// Ollama exposes an OpenAI-compatible REST API, so this is a thin wrapper that
// sets the correct base URL and a dummy API key (Ollama ignores auth).
// baseURL defaults to http://localhost:11434/v1 (respects OLLAMA_HOST env var).
func NewOllamaProvider(baseURL string) (*OpenAIProvider, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = ollamaBaseURL()
	}
	return NewOpenAIProvider("ollama", baseURL)
}

// NewOllamaProviderFromEnv creates an Ollama provider using OLLAMA_HOST env var,
// falling back to http://localhost:11434/v1.
func NewOllamaProviderFromEnv() (*OpenAIProvider, error) {
	return NewOllamaProvider("")
}

func ollamaBaseURL() string {
	host := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if host == "" {
		return "http://localhost:11434/v1"
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	return fmt.Sprintf("%s/v1", strings.TrimRight(host, "/"))
}
