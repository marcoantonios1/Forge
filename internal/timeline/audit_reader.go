package timeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// auditEntry mirrors mode.AuditLogger's JSONL shape exactly (Ts, Type, Detail)
// — duplicated here rather than importing internal/mode to avoid a needless
// coupling for a 3-field struct in either direction.
type auditEntry struct {
	Ts     string `json:"ts"`
	Type   string `json:"type"`   // "tool" | "patch" | "git" — coarse, NOT the original event.Type
	Detail string `json:"detail"` // JSON-encoded original event.Payload
}

// ReadAuditLog reads a .forge/logs/<sessionID>.jsonl file and converts each
// line into a Row. Because the audit log stores only the coarse tool/patch/git
// classification rather than the original fine-grained event.Type, EventType
// in the resulting rows reflects that coarser shape — this is an accepted gap
// between the in-memory path (which has the full original event.Type) and this
// disk-based path.
//
// TODO: extend the audit log schema to store the original fine-grained
// event.Type alongside the coarse classification, so `forge logs show` can
// eventually match describeStep's precision.
func ReadAuditLog(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	var rows []Row
	var prevTs time.Time
	first := true

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry auditEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // skip malformed lines rather than failing the whole read
		}
		ts, err := time.Parse(time.RFC3339, entry.Ts)
		if err != nil {
			continue
		}
		var dur time.Duration
		if !first {
			dur = ts.Sub(prevTs)
		}
		first = false
		prevTs = ts

		detail := entry.Detail
		if len(detail) > 100 {
			detail = detail[:100] + "..."
		}

		rows = append(rows, Row{
			Timestamp: ts,
			Duration:  dur,
			EventType: entry.Type,
			Detail:    detail,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading audit log: %w", err)
	}
	return rows, nil
}

// LogPath builds the conventional path for a session's audit log, mirroring
// mode.NewAuditLogger's path construction exactly.
func LogPath(root, sessionID string) string {
	return filepath.Join(root, ".forge", "logs", sessionID+".jsonl")
}
