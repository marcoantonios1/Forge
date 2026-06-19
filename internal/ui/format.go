package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcoantonios1/Forge/internal/events"
)

// glyph returns the unicode symbol when colour is true, ASCII fallback otherwise.
func glyph(unicode, ascii string, colour bool) string {
	if colour {
		return unicode
	}
	return ascii
}

// str safely extracts a string from a payload map.
func str(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	if v == "" {
		return "<unknown>"
	}
	return v
}

// num safely extracts an int from a payload value (handles int and float64).
func num(payload map[string]any, key string) int {
	switch v := payload[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func formatTaskStarted(e events.Event, colour bool) string {
	sessionID := str(e.Payload, "session_id")
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}

	category, scope := "<unknown>", "<unknown>"
	if taskStr, ok := e.Payload["task"].(string); ok {
		var t struct {
			Category string `json:"category"`
			Scope    string `json:"scope"`
		}
		if json.Unmarshal([]byte(taskStr), &t) == nil {
			if t.Category != "" {
				category = t.Category
			}
			if t.Scope != "" {
				scope = t.Scope
			}
		}
	}

	g := glyph("⚙", ">", colour)
	label := fmt.Sprintf("Starting task  %s/%s", category, scope)
	return fmt.Sprintf("  %s  %s  [%s]", g, label, short)
}

func formatToolInvoked(e events.Event, colour bool) string {
	toolName := str(e.Payload, "tool")
	args, _ := e.Payload["args"].(map[string]any)

	summary := argSummary(toolName, args)
	g := glyph("→", ">", colour)
	name := Colour(toolName, Cyan, colour)

	if summary != "" {
		return fmt.Sprintf("  %s  %s  %s", g, name, DimText(summary, colour))
	}
	return fmt.Sprintf("  %s  %s", g, name)
}

func argSummary(toolName string, args map[string]any) string {
	if args == nil {
		return ""
	}
	switch {
	case strings.HasPrefix(toolName, "git_"):
		return "(repo)"
	case toolName == "read_file":
		if p, ok := args["path"].(string); ok {
			return p
		}
	case toolName == "search_code":
		if p, ok := args["pattern"].(string); ok {
			return p
		}
	case toolName == "list_files":
		if r, ok := args["root"].(string); ok {
			return r
		}
	}
	return ""
}

func formatToolOutput(e events.Event, colour bool) string {
	toolName := str(e.Payload, "tool")
	ok, _ := e.Payload["ok"].(bool)
	summary := str(e.Payload, "summary")

	if ok {
		g := glyph("✓", ">", colour)
		return fmt.Sprintf("  %s  %s  %s", g, toolName, DimText(summary, colour))
	}
	g := glyph("✗", "x", colour)
	return fmt.Sprintf("  %s  %s  %s", g, toolName, Colour(summary, Red, colour))
}

func formatPatchReviewed(e events.Event, colour bool) string {
	ok, _ := e.Payload["ok"].(bool)
	reason, _ := e.Payload["reason"].(string)
	if ok {
		note := reason
		if note == "" {
			note = "ok"
		}
		label := DimText("🔍 Reviewed:", colour)
		return fmt.Sprintf("  %s %s", label, note)
	}
	if reason == "" {
		reason = "unknown"
	}
	label := Colour("⚠ Review flagged:", Yellow, colour)
	return fmt.Sprintf("  %s %s — retrying", label, reason)
}

func formatFilePatchCreated(e events.Event, colour bool) string {
	files := extractStringSlice(e.Payload["files"])
	g := glyph("✎", "~", colour)
	label := Colour("Patch ready", Yellow, colour)
	return fmt.Sprintf("  %s  %s  %d file(s)", g, label, len(files))
}

func formatFilePatchApplied(e events.Event, colour bool) string {
	path := str(e.Payload, "path")
	hunks := num(e.Payload, "hunks")
	g := glyph("✔", "+", colour)
	label := Colour("Applied", Green, colour)
	return fmt.Sprintf("  %s  %s  %s  (%d hunks)", g, label, Colour(path, Bold, colour), hunks)
}

func formatFilePatchReverted(e events.Event, colour bool) string {
	path := str(e.Payload, "path")
	g := glyph("↩", "<", colour)
	label := Colour("Reverted", Yellow, colour)
	return fmt.Sprintf("  %s  %s  %s", g, label, path)
}

func formatFilePatchFailed(e events.Event, colour bool) string {
	path := str(e.Payload, "path")
	errMsg := str(e.Payload, "error")
	g := glyph("✘", "x", colour)
	label := Colour("Patch failed", Red, colour)
	return fmt.Sprintf("  %s  %s  %s: %s", g, label, path, errMsg)
}

func formatTaskFailed(e events.Event, colour bool) string {
	reason := str(e.Payload, "reason")
	g := glyph("✘", "x", colour)
	label := Colour("Task failed:", Red, colour)
	return fmt.Sprintf("  %s  %s %s", g, label, Colour(reason, Bold, colour))
}

func formatClarificationAsked(e events.Event, colour bool) string {
	question := str(e.Payload, "question")
	prefix := Colour("?", Yellow, colour)
	return fmt.Sprintf("\n  %s  Clarification needed: %s", prefix, Colour(question, Bold, colour))
}

func formatClarificationAnswered(e events.Event, colour bool) string {
	refined, _ := e.Payload["refined"].(bool)
	label := "original task"
	if refined {
		label = str(e.Payload, "answer")
	}
	return fmt.Sprintf("  %s  Proceeding with %s", DimText("↳", colour), label)
}

func formatGitBranch(e events.Event, colour bool) string {
	branch := str(e.Payload, "branch")
	g := glyph("⎇", ">", colour)
	return fmt.Sprintf("  %s  Branch  %s", g, Colour(branch, Cyan, colour))
}

func formatGitCommit(e events.Event, colour bool) string {
	hash := str(e.Payload, "hash")
	// Only show the subject line — the body may contain a "Files:" list.
	msg := strings.SplitN(str(e.Payload, "message"), "\n", 2)[0]
	g := glyph("✦", "*", colour)
	return fmt.Sprintf("  %s  Committed  %s %s",
		g, DimText("["+hash+"]", colour), Colour(msg, Bold, colour))
}

func formatGitPush(e events.Event, colour bool) string {
	branch := str(e.Payload, "branch")
	remote := str(e.Payload, "remote")
	g := glyph("↑", "^", colour)
	return fmt.Sprintf("  %s  Pushed  %s  → %s", g, Colour(branch, Cyan, colour), remote)
}

func formatGitStash(e events.Event, colour bool) string {
	action := str(e.Payload, "action")
	g := glyph("≡", "~", colour)
	return fmt.Sprintf("  %s  Stash %s", g, Colour(action, Yellow, colour))
}

func formatCommandStarted(e events.Event, colour bool) string {
	command := str(e.Payload, "command")
	g := Colour(glyph("▶", ">", colour), Yellow, colour)
	return fmt.Sprintf("  %s  run  %s", g, Colour(command, Bold, colour))
}

func formatCommandOutput(e events.Event, colour bool) string {
	line, _ := e.Payload["line"].(string)
	stream := str(e.Payload, "stream")
	if stream == "stderr" {
		return "     " + Colour(line, Yellow, colour)
	}
	return "     " + Colour(line, White, colour)
}

func formatCommandFinished(e events.Event, colour bool) string {
	exitCode := num(e.Payload, "exit_code")
	timedOut, _ := e.Payload["timed_out"].(bool)
	durationMs := int64(num(e.Payload, "duration_ms"))

	if timedOut {
		g := glyph("✘", "x", colour)
		return Colour(fmt.Sprintf("  %s  timed out  (%dms)", g, durationMs), Red, colour)
	}
	if exitCode == 0 {
		g := glyph("✓", ">", colour)
		return Colour(fmt.Sprintf("  %s  done  exit 0  (%dms)", g, durationMs), Green, colour)
	}
	g := glyph("✘", "x", colour)
	return Colour(fmt.Sprintf("  %s  done  exit %d  (%dms)", g, exitCode, durationMs), Red, colour)
}

func extractStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		var out []string
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}
