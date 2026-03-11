// Package analyze - provider_openai.go implements pure OpenAI provider.
package analyze

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements Provider for OpenAI API.
type OpenAIProvider struct {
	client       *openai.Client
	model        string
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
	calls        atomic.Int64
}

// NewOpenAIProvider creates an OpenAI provider.
// Reads config from environment:
//   - OPENAI_API_KEY (required)
//   - OPENAI_BASE_URL (optional, for proxies/compatible APIs)
func NewOpenAIProvider(model string) (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")

	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	// Default model
	if model == "" {
		model = "gpt-4o-mini"
	}

	var client *openai.Client
	if baseURL != "" {
		config := openai.DefaultConfig(apiKey)
		config.BaseURL = baseURL
		client = openai.NewClientWithConfig(config)
	} else {
		client = openai.NewClient(apiKey)
	}

	return &OpenAIProvider{
		client: client,
		model:  model,
	}, nil
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

func (p *OpenAIProvider) Call(ctx context.Context, prompt string, maxTokens int64) (string, error) {
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
		return "", fmt.Errorf("openai: %w", err)
	}

	p.inputTokens.Add(int64(resp.Usage.PromptTokens))
	p.outputTokens.Add(int64(resp.Usage.CompletionTokens))
	p.calls.Add(1)

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai: no response choices")
	}

	return resp.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) GetUsage() Usage {
	return Usage{
		InputTokens:  p.inputTokens.Load(),
		OutputTokens: p.outputTokens.Load(),
		Calls:        p.calls.Load(),
	}
}
