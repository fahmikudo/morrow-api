package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"morrow-api/internal/config"
)

// TestConnection checks the user's key against `/models` without spending
// tokens.
func TestConnection(ctx context.Context, client *http.Client, cfg *config.Config, apiKey string) (bool, string) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return false, "DeepSeek API key is not configured"
	}

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, cfg.DeepSeekBaseURL+"/models", nil)
	if err != nil {
		return false, fmt.Sprintf("Connection failed (%s)", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)

	response, err := client.Do(request)
	if err != nil {
		return false, fmt.Sprintf("Connection failed (%s)", err)
	}
	defer func() {
		_ = response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
	}()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true, "Connected"
	}

	return false, fmt.Sprintf(
		"Connection failed (HTTP %d %s)",
		response.StatusCode,
		http.StatusText(response.StatusCode),
	)
}

// Tool is an OpenAI-compatible function definition the model may call.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes one callable function.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall is one function call the model requested.
type ToolCall struct {
	Name      string
	Arguments string
}

// Completion is the model's answer plus any tool calls it made.
type Completion struct {
	Content   string
	ToolCalls []ToolCall
}

// Complete asks the configured model for an answer, optionally exposing tools.
// Legacy aliases reject provider-specific fields, so tools are only offered to
// models from the current catalogue.
func Complete(
	ctx context.Context,
	client *http.Client,
	cfg *config.Config,
	record AIRow,
	apiKey string,
	system string,
	message string,
	tools []Tool,
) (Completion, error) {
	body := map[string]any{
		"model": record.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": message},
		},
		"temperature": 0.2,
		"max_tokens":  700,
	}

	legacy := contains(LegacyModels, record.Model)
	if record.ReasoningEffort != nil && !legacy {
		body["reasoning_effort"] = *record.ReasoningEffort
	}
	if len(tools) > 0 && !legacy {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return Completion{}, err
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		cfg.DeepSeekBaseURL+"/chat/completions",
		bytes.NewReader(encoded),
	)
	if err != nil {
		return Completion{}, err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return Completion{}, err
	}
	defer func() {
		_ = response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Completion{}, fmt.Errorf("status %d", response.StatusCode)
	}

	var payload struct {
		Choices []struct {
			Message struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Completion{}, err
	}

	if len(payload.Choices) == 0 {
		return Completion{}, fmt.Errorf("missing completion")
	}

	completion := Completion{}
	if payload.Choices[0].Message.Content != nil {
		completion.Content = *payload.Choices[0].Message.Content
	}
	for _, call := range payload.Choices[0].Message.ToolCalls {
		if call.Function.Name == "" {
			continue
		}
		completion.ToolCalls = append(completion.ToolCalls, ToolCall{
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}

	if strings.TrimSpace(completion.Content) == "" && len(completion.ToolCalls) == 0 {
		return Completion{}, fmt.Errorf("missing completion")
	}

	return completion, nil
}
