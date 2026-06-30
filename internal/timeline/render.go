package timeline

import (
	"fmt"
	"io"
	"time"
)

// Row is one renderable timeline line.
type Row struct {
	Timestamp time.Time
	Duration  time.Duration // delta from the previous row's timestamp; first row = 0
	EventType string
	Detail    string
}

// BuildRows converts buffered Steps into Rows, computing each row's duration
// as the delta from the immediately preceding step's timestamp.
func BuildRows(steps []Step) []Row {
	rows := make([]Row, 0, len(steps))
	var prev time.Time
	for i, s := range steps {
		var dur time.Duration
		if i > 0 {
			dur = s.Timestamp.Sub(prev)
		}
		rows = append(rows, Row{
			Timestamp: s.Timestamp,
			Duration:  dur,
			EventType: s.Type,
			Detail:    describeStep(s),
		})
		prev = s.Timestamp
	}
	return rows
}

// describeStep produces a one-line description for a Step, mirroring the
// event vocabulary that internal/ui/renderer.go already handles live.
func describeStep(s Step) string {
	get := func(key string) string {
		v, _ := s.Payload[key].(string)
		return v
	}
	getBool := func(key string) bool {
		v, _ := s.Payload[key].(bool)
		return v
	}
	switch s.Type {
	case "task.started":
		return "task started"
	case "tool.invoked":
		return "tool call: " + get("tool")
	case "tool.output":
		if getBool("ok") {
			return "tool result: " + get("tool") + " ok"
		}
		return "tool result: " + get("tool") + " error: " + get("summary")
	case "patch.reviewed":
		if getBool("ok") {
			return "reviewer: OK — " + get("reason")
		}
		return "reviewer: REJECTED — " + get("reason")
	case "file.patch.created":
		return "patch proposed"
	case "file.patch.applied":
		return "patch applied: " + get("path")
	case "file.patch.reverted":
		return "patch reverted: " + get("path")
	case "file.patch.failed":
		return "patch failed: " + get("path") + " — " + get("error")
	case "task.completed":
		return "task completed: " + get("summary")
	case "task.failed":
		return "task failed: " + get("reason")
	case "clarification.asked":
		return "clarification asked: " + get("question")
	case "clarification.answered":
		return "clarification answered"
	case "git.branch":
		return "git branch: " + get("branch")
	case "git.commit":
		return "git commit: " + get("hash") + " " + get("message")
	case "git.push":
		return "git push: " + get("remote") + "/" + get("branch")
	case "git.stash":
		return "git stash: " + get("action")
	case "command.started":
		return "command: " + get("command")
	case "command.finished":
		return fmt.Sprintf("command finished: exit %v", s.Payload["exit_code"])
	case "confirm.decision":
		return "confirm: " + get("decision")
	default:
		return s.Type
	}
}

// RenderTable prints rows as a plain-text aligned table to out. No ANSI
// colour codes — the timeline is always plain text regardless of terminal
// capability.
//
// TODO: support `forge logs show <id> --json` and an equivalent
// --timeline-json flag for machine-readable timeline output.
func RenderTable(out io.Writer, rows []Row) {
	fmt.Fprintln(out, "\n── Execution timeline ──────────────────────────────")
	fmt.Fprintf(out, "%-12s  %10s  %-22s  %s\n", "time", "+duration", "event", "detail")
	for _, r := range rows {
		fmt.Fprintf(out, "%-12s  %10s  %-22s  %s\n",
			r.Timestamp.Format("15:04:05.000"),
			r.Duration.Round(time.Millisecond),
			r.EventType,
			r.Detail)
	}
	fmt.Fprintln(out, "─────────────────────────────────────────────────────")
}
