package mode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcoantonios1/Forge/internal/events"
)

// AuditLogger appends structured JSONL entries to .forge/logs/<sessionID>.jsonl.
// It is only active in autonomous mode; a nil *AuditLogger is a safe no-op.
type AuditLogger struct {
	f  *os.File
	mu sync.Mutex
}

// NewAuditLogger opens (creating if necessary) root/.forge/logs/<sessionID>.jsonl
// for appending. Returns (nil, nil) when mode is not ModeAutonomous — callers
// can safely call Log() on a nil *AuditLogger.
//
// TODO: add a `forge logs show <session-id>` subcommand to pretty-print an
// audit log, mirroring `forge memory show`.
func NewAuditLogger(root, sessionID string, m SessionMode) (*AuditLogger, error) {
	if m != ModeAutonomous {
		return nil, nil
	}
	// TODO: autonomous mode's automatic git workflow could still respect
	// --allow-main-commit's existing guard against committing directly to
	// main/master. That guard lives in runGitWorkflow (unchanged here) and
	// remains active regardless of --mode, so the constraint is already met.
	dir := filepath.Join(root, ".forge", "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mode: creating log dir: %w", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("mode: opening audit log: %w", err)
	}
	return &AuditLogger{f: f}, nil
}

type auditEntry struct {
	Ts     string `json:"ts"`
	Type   string `json:"type"`   // "tool" | "patch" | "git"
	Detail string `json:"detail"`
}

// Log appends one JSONL entry. Safe to call on a nil *AuditLogger (no-op).
func (a *AuditLogger) Log(entryType, detail string) {
	if a == nil || a.f == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	entry := auditEntry{
		Ts:     time.Now().UTC().Format(time.RFC3339),
		Type:   entryType,
		Detail: detail,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	a.f.Write(append(b, '\n')) //nolint:errcheck
}

// Close releases the underlying file handle. Safe to call on nil.
func (a *AuditLogger) Close() error {
	if a == nil || a.f == nil {
		return nil
	}
	return a.f.Close()
}

// EmitterTap wraps an events.Emitter so that every audit-relevant event is
// also appended to the audit log. This lets main.go get audit logging for
// free by wrapping the existing emitter rather than threading AuditLogger
// through agent/tools code.
type EmitterTap struct {
	Inner events.Emitter
	Audit *AuditLogger
}

func (t *EmitterTap) Emit(e events.Event) {
	t.Inner.Emit(e)
	if t.Audit == nil {
		return
	}
	var entryType string
	switch {
	case strings.HasPrefix(string(e.Type), "tool."):
		entryType = "tool"
	case strings.HasPrefix(string(e.Type), "file.patch."):
		entryType = "patch"
	case strings.HasPrefix(string(e.Type), "git."),
		strings.HasPrefix(string(e.Type), "command."):
		entryType = "git"
	default:
		return // task.started, clarification.*, etc. are not audit-relevant
	}
	detailBytes, _ := json.Marshal(e.Payload)
	t.Audit.Log(entryType, string(detailBytes))
}
