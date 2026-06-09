package events

import "time"

type EventType string

const (
	EventToolInvoked  EventType = "tool.invoked"
	EventToolOutput   EventType = "tool.output"
	EventTaskStarted  EventType = "task.started"
	EventPlanCreated  EventType = "plan.created"
	EventTaskComplete EventType = "task.completed"
	EventTaskFailed   EventType = "task.failed"

	EventFilePatchCreated  EventType = "file.patch.created"
	EventFilePatchApplied  EventType = "file.patch.applied"
	EventFilePatchFailed   EventType = "file.patch.failed"
	EventFilePatchReverted EventType = "file.patch.reverted"
)

type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	Payload   map[string]any `json:"payload"`
}

type Emitter interface {
	Emit(e Event)
}

func ToolInvokedEvent(sessionID, toolName string, args map[string]any) Event {
	return Event{
		Type:      EventToolInvoked,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"tool": toolName,
			"args": args,
		},
	}
}

func ToolOutputEvent(sessionID, toolName string, ok bool, summary string) Event {
	return Event{
		Type:      EventToolOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"tool":    toolName,
			"ok":      ok,
			"summary": summary,
		},
	}
}
