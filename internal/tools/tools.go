package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/marcoantonios1/Forge/internal/events"
)

type Tool interface {
	Name() string
	Run(ctx context.Context, args map[string]any) (any, error)
}

// ToolCall is a request to invoke a named tool with given arguments.
type ToolCall struct {
	Name string
	Args map[string]any
}

type ToolError struct {
	Tool    string
	Message string
	Err     error
}

func (e *ToolError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Tool, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Tool, e.Message)
}

func (e *ToolError) Unwrap() error {
	return e.Err
}

func IsRecoverable(err error) bool {
	var te *ToolError
	return errors.As(err, &te)
}

type ToolRunner struct {
	tool      Tool
	emitter   events.Emitter
	sessionID string
}

func NewRunner(t Tool, emitter events.Emitter, sessionID string) *ToolRunner {
	return &ToolRunner{tool: t, emitter: emitter, sessionID: sessionID}
}

func (r *ToolRunner) Run(ctx context.Context, args map[string]any) (any, error) {
	r.emitter.Emit(events.ToolInvokedEvent(r.sessionID, r.tool.Name(), args))

	result, err := r.tool.Run(ctx, args)

	if err != nil {
		r.emitter.Emit(events.ToolOutputEvent(r.sessionID, r.tool.Name(), false, err.Error()))
	} else {
		r.emitter.Emit(events.ToolOutputEvent(r.sessionID, r.tool.Name(), true, summarise(result)))
	}

	return result, err
}

func summarise(result any) string {
	switch v := result.(type) {
	case interface{ Summary() string }:
		return v.Summary()
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%T", result)
	}
}
