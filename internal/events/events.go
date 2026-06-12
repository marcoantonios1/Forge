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

	EventConfirmDecision EventType = "confirm.decision"

	EventPermissionGranted EventType = "permission.granted"
	EventPermissionDenied  EventType = "permission.denied"

	EventGitBranch EventType = "git.branch"
	EventGitCommit EventType = "git.commit"
	EventGitPush   EventType = "git.push"
	EventGitStash  EventType = "git.stash"
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

// PermissionGrantedEvent builds a permission.granted event.
// scope: "pre-approved" | "session" | "once"
func PermissionGrantedEvent(sessionID, tool, category, scope string) Event {
	return Event{
		Type:      EventPermissionGranted,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"tool":     tool,
			"category": category,
			"scope":    scope,
		},
	}
}

// PermissionDeniedEvent builds a permission.denied event.
func PermissionDeniedEvent(sessionID, tool, category string) Event {
	return Event{
		Type:      EventPermissionDenied,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"tool":     tool,
			"category": category,
		},
	}
}

func GitBranchEvent(sessionID, branch string, checkout bool) Event {
	return Event{
		Type:      EventGitBranch,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "branch": branch, "checkout": checkout},
	}
}

func GitCommitEvent(sessionID, hash, message string, files int) Event {
	return Event{
		Type:      EventGitCommit,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "hash": hash, "message": message, "files": files},
	}
}

func GitPushEvent(sessionID, remote, branch string) Event {
	return Event{
		Type:      EventGitPush,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "remote": remote, "branch": branch},
	}
}

func GitStashEvent(sessionID, action string) Event {
	return Event{
		Type:      EventGitStash,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "action": action},
	}
}

// Multi fans out Emit calls to all provided emitters.
type multiEmitter struct{ emitters []Emitter }

func Multi(emitters ...Emitter) Emitter {
	return &multiEmitter{emitters: emitters}
}

func (m *multiEmitter) Emit(e Event) {
	for _, em := range m.emitters {
		em.Emit(e)
	}
}
