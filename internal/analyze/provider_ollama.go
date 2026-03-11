// Package analyze - provider_ollama.go implements Ollama local provider.
package analyze

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/sashabaranov/go-openai"
)

// OllamaProvider implements Provider for Ollama local inference.
// Ollama exposes an OpenAI-compatible API, so we reuse the OpenAI client.
type OllamaProvider struct {
	client       *openai.Client
	model        string
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
	calls        atomic.Int64
}

// NewOllamaProvider creates an Ollama provider.
// Reads config from environment:
//   - OLLAMA_HOST (optional, default: http://localhost:11434)
//   - OLLAMA_MODEL (optional, default: llama3.2)
func NewOllamaProvider(model string) (*OllamaProvider, error) {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}

	// Default model for Ollama
	if model == "" {
		model = os.Getenv("OLLAMA_MODEL")
		if model == "" {
			model = "llama3.2"
		}
	}

	// Ollama uses OpenAI-compatible API at /v1
	config := openai.DefaultConfig("ollama") // Ollama doesn't need API key
	config.BaseURL = host + "/v1"

	return &OllamaProvider{
		client: openai.NewClientWithConfig(config),
		model:  model,
	}, nil
}

func (p *OllamaProvider) Name() string {
	return "ollama"
}

func (p *OllamaProvider) Call(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: p.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		MaxTokens: int(maxTokens),
	})
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}

	// Ollama may not always return usage stats
	if resp.Usage.PromptTokens > 0 {
		p.inputTokens.Add(int64(resp.Usage.PromptTokens))
		p.outputTokens.Add(int64(resp.Usage.CompletionTokens))
	}
	p.calls.Add(1)

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("ollama: no response choices")
	}

	return resp.Choices[0].Message.Content, nil
}

func (p *OllamaProvider) GetUsage() Usage {
	return Usage{
		InputTokens:  p.inputTokens.Load(),
		OutputTokens: p.outputTokens.Load(),
		Calls:        p.calls.Load(),
	}
}
