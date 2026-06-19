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

	EventClarificationAsked    EventType = "clarification.asked"
	EventClarificationAnswered EventType = "clarification.answered"

	EventGitBranch EventType = "git.branch"
	EventGitCommit EventType = "git.commit"
	EventGitPush   EventType = "git.push"
	EventGitStash  EventType = "git.stash"

	EventCommandStarted  EventType = "command.started"
	EventCommandOutput   EventType = "command.output"
	EventCommandFinished EventType = "command.finished"

	EventPatchReviewed EventType = "patch.reviewed"
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

func ClarificationAskedEvent(sessionID, question, taskRaw string) Event {
	return Event{
		Type:      EventClarificationAsked,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "question": question, "task_raw": taskRaw},
	}
}

func ClarificationAnsweredEvent(sessionID, answer string, refined bool) Event {
	return Event{
		Type:      EventClarificationAnswered,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "answer": answer, "refined": refined},
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

func PatchReviewedEvent(sessionID string, ok bool, reason string) Event {
	return Event{
		Type:      EventPatchReviewed,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "ok": ok, "reason": reason},
	}
}

func CommandStartedEvent(sessionID, command, root string) Event {
	return Event{
		Type:      EventCommandStarted,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "command": command, "root": root},
	}
}

func CommandOutputEvent(sessionID, command, stream, line string) Event {
	return Event{
		Type:      EventCommandOutput,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload:   map[string]any{"session_id": sessionID, "command": command, "stream": stream, "line": line},
	}
}

func CommandFinishedEvent(sessionID, command string, exitCode int, timedOut bool, durationMs int64) Event {
	return Event{
		Type:      EventCommandFinished,
		Timestamp: time.Now(),
		SessionID: sessionID,
		Payload: map[string]any{
			"session_id":  sessionID,
			"command":     command,
			"exit_code":   exitCode,
			"timed_out":   timedOut,
			"duration_ms": durationMs,
		},
	}
}
