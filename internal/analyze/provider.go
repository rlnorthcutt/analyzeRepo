// Package analyze - provider.go defines the LLM provider interface
// that abstracts different AI backends (Anthropic, Azure, OpenAI, Ollama).
package analyze

import (
	"context"
	"fmt"
	"os"
)

// Provider abstracts LLM API calls across different backends.
// Each provider implementation handles its own authentication and API format.
type Provider interface {
	// Name returns the provider identifier (e.g., "anthropic", "azure", "openai", "ollama")
	Name() string

	// Call sends a prompt and returns the text response.
	Call(ctx context.Context, prompt string, maxTokens int64) (string, error)

	// GetUsage returns accumulated token usage for this session.
	GetUsage() Usage
}

// ProviderConfig holds configuration for provider selection.
type ProviderConfig struct {
	// Provider name override. If empty, auto-detect from environment.
	Provider string

	// Model override. If empty, use provider default.
	Model string
}

// DetectProvider auto-detects the best available provider from environment variables.
// Priority: Azure > OpenAI > Anthropic > Ollama > Claude CLI
func DetectProvider(cfg ProviderConfig) (Provider, error) {
	// If explicitly specified, use that provider
	if cfg.Provider != "" {
		return createProvider(cfg.Provider, cfg.Model)
	}

	// Auto-detect based on environment variables
	// Azure takes priority (enterprise users)
	if os.Getenv("AZURE_OPENAI_ENDPOINT") != "" && os.Getenv("AZURE_OPENAI_API_KEY") != "" {
		return NewAzureProvider(cfg.Model)
	}

	// Pure OpenAI
	if os.Getenv("OPENAI_API_KEY") != "" && os.Getenv("AZURE_OPENAI_ENDPOINT") == "" {
		return NewOpenAIProvider(cfg.Model)
	}

	// Anthropic
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return NewAnthropicProvider(cfg.Model)
	}

	// Ollama (local)
	if os.Getenv("OLLAMA_HOST") != "" || isOllamaRunning() {
		return NewOllamaProvider(cfg.Model)
	}

	// No provider found
	return nil, fmt.Errorf(`no LLM provider detected

Set one of:
  AZURE_OPENAI_ENDPOINT + AZURE_OPENAI_API_KEY  (Azure OpenAI)
  OPENAI_API_KEY                                 (OpenAI)
  ANTHROPIC_API_KEY                              (Anthropic)
  OLLAMA_HOST                                    (Ollama local)`)
}

// createProvider creates a specific provider by name.
func createProvider(name, model string) (Provider, error) {
	switch name {
	case "azure":
		return NewAzureProvider(model)
	case "openai":
		return NewOpenAIProvider(model)
	case "anthropic":
		return NewAnthropicProvider(model)
	case "ollama":
		return NewOllamaProvider(model)
	default:
		return nil, fmt.Errorf("unknown provider: %s (valid: azure, openai, anthropic, ollama)", name)
	}
}

// isOllamaRunning checks if Ollama is running locally.
func isOllamaRunning() bool {
	// Simple check - try to connect to default Ollama port
	// This is a placeholder - real implementation would do HTTP check
	return false
}
