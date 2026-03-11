// Package analyze - provider_anthropic.go implements Anthropic Claude provider.
package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	anthropic "github.com/anthropics/anthropic-sdk-go"
)

// AnthropicProvider implements Provider for Anthropic Claude API.
type AnthropicProvider struct {
	client       *anthropic.Client
	model        string
	hasCLI       bool
	inputTokens  atomic.Int64
	outputTokens atomic.Int64
	calls        atomic.Int64
}

// NewAnthropicProvider creates an Anthropic provider.
// Reads config from environment:
//   - ANTHROPIC_API_KEY (optional if Claude CLI is available)
func NewAnthropicProvider(model string) (*AnthropicProvider, error) {
	p := &AnthropicProvider{}

	// Default model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	p.model = model

	// Check for API key
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		client := anthropic.NewClient()
		p.client = &client
	}

	// Check for Claude CLI as fallback
	if _, err := exec.LookPath("claude"); err == nil {
		p.hasCLI = true
	}

	if p.client == nil && !p.hasCLI {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required (or install Claude CLI)")
	}

	return p, nil
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

func (p *AnthropicProvider) Call(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	// Try API first
	if p.client != nil {
		text, err := p.callAPI(ctx, prompt, maxTokens)
		if err != nil {
			// Fall back to CLI on auth errors
			if p.hasCLI && isAuthError(err) {
				return p.callCLI(ctx, prompt)
			}
			return "", err
		}
		return text, nil
	}

	// Use CLI
	return p.callCLI(ctx, prompt)
}

func (p *AnthropicProvider) callAPI(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}

	p.inputTokens.Add(resp.Usage.InputTokens)
	p.outputTokens.Add(resp.Usage.OutputTokens)
	p.calls.Add(1)

	var sb strings.Builder
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			sb.WriteString(v.Text)
		}
	}
	return sb.String(), nil
}

// cliJSONResponse is the structure returned by `claude --output-format json`.
type cliJSONResponse struct {
	Result string `json:"result"`
	Usage  struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (p *AnthropicProvider) callCLI(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", "--no-session-persistence",
		"--output-format", "json", "--tools", "")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = filterEnv("CLAUDECODE")

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("claude CLI: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("claude CLI: %w", err)
	}

	var parsed cliJSONResponse
	if jsonErr := json.Unmarshal(out, &parsed); jsonErr != nil {
		return strings.TrimSpace(string(out)), nil
	}

	p.inputTokens.Add(parsed.Usage.InputTokens)
	p.outputTokens.Add(parsed.Usage.OutputTokens)
	p.calls.Add(1)

	return parsed.Result, nil
}

func (p *AnthropicProvider) GetUsage() Usage {
	return Usage{
		InputTokens:  p.inputTokens.Load(),
		OutputTokens: p.outputTokens.Load(),
		Calls:        p.calls.Load(),
	}
}

// isAuthError reports whether err indicates an HTTP 401 authentication failure.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "authentication_error") ||
		strings.Contains(msg, "status 401") ||
		strings.Contains(msg, "401")
}

// filterEnv returns os.Environ() with the specified key removed.
func filterEnv(key string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	prefix := key + "="
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}
