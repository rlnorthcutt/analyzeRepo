// Package analyze - provider_azure.go implements Azure OpenAI provider.
package analyze

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/sashabaranov/go-openai"
)

// AzureProvider implements Provider for Azure OpenAI Service.
type AzureProvider struct {
	client       *openai.Client
	model        string
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
	calls        atomic.Int64
}

// NewAzureProvider creates an Azure OpenAI provider.
// Reads config from environment:
//   - AZURE_OPENAI_ENDPOINT (required)
//   - AZURE_OPENAI_API_KEY (required)
//   - OPENAI_API_VERSION (optional, default: 2024-12-01-preview)
func NewAzureProvider(model string) (*AzureProvider, error) {
	endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	apiVersion := os.Getenv("OPENAI_API_VERSION")

	if endpoint == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY is required")
	}
	if apiVersion == "" {
		apiVersion = "2024-12-01-preview" // Default to latest stable
	}

	// Default model for Azure
	if model == "" {
		model = "gpt-4o-mini"
	}

	config := openai.DefaultAzureConfig(apiKey, endpoint)
	config.APIVersion = apiVersion

	return &AzureProvider{
		client: openai.NewClientWithConfig(config),
		model:  model,
	}, nil
}

func (p *AzureProvider) Name() string {
	return "azure"
}

func (p *AzureProvider) Call(ctx context.Context, prompt string, maxTokens int64) (string, error) {
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
		return "", fmt.Errorf("azure openai: %w", err)
	}

	// Track usage
	p.inputTokens.Add(int64(resp.Usage.PromptTokens))
	p.outputTokens.Add(int64(resp.Usage.CompletionTokens))
	p.calls.Add(1)

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("azure openai: no response choices")
	}

	return resp.Choices[0].Message.Content, nil
}

func (p *AzureProvider) GetUsage() Usage {
	return Usage{
		InputTokens:  p.inputTokens.Load(),
		OutputTokens: p.outputTokens.Load(),
		Calls:        p.calls.Load(),
	}
}
