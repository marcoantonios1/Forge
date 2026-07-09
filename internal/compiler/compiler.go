package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/marcoantonios1/Forge/internal/costguard"
)

type Compiler struct {
	client *costguard.Client
	model  string
	debug  bool
}

func New(client *costguard.Client, model string, debug bool) *Compiler {
	return &Compiler{client: client, model: model, debug: debug}
}

func (c *Compiler) Compile(ctx context.Context, rawInput string) (*Task, error) {
	rawInput = strings.TrimSpace(rawInput)
	if rawInput == "" {
		return nil, errors.New("compiler: input is empty")
	}

	req := costguard.ChatRequest{
		Model: c.model,
		Messages: []costguard.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: rawInput},
		},
	}

	resp, err := c.client.Chat(ctx, req)
	if err != nil {
		switch {
		case costguard.IsBudgetExceeded(err):
			return nil, fmt.Errorf("budget limit reached: %w", err)
		case costguard.IsProviderDown(err):
			return nil, fmt.Errorf("model provider unavailable, try again: %w", err)
		}
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("compiler: empty response from model")
	}

	raw := resp.Choices[0].Message.Content
	if c.debug {
		fmt.Fprintf(os.Stderr, "[compiler] raw model response:\n%s\n", raw)
	}
	jsonStr, err := extractJSON(raw)
	if err != nil {
		return nil, err
	}

	var task Task
	if err := json.Unmarshal([]byte(jsonStr), &task); err != nil {
		return nil, fmt.Errorf("compiler: unmarshal task: %w", err)
	}
	task.Type = "engineering_task"
	task.RawInput = rawInput

	// Ensure nil slices become empty slices for consistent JSON output.
	if task.Constraints == nil {
		task.Constraints = []string{}
	}
	if task.Deliverables == nil {
		task.Deliverables = []string{}
	}

	if err := task.Validate(); err != nil {
		return nil, err
	}

	// Clarification is handled in agent.clarify() — see internal/agent/agent.go.

	if c.debug {
		pretty, _ := json.MarshalIndent(&task, "", "  ")
		fmt.Fprintf(os.Stderr, "[compiler] compiled task:\n%s\n", pretty)
	}

	return &task, nil
}
