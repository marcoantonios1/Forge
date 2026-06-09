package ui

import (
	"fmt"
	"strings"
)

// RenderDiff formats a unified diff string for terminal output.
// Indents every line with two spaces. Returns the formatted string (no trailing newline).
func RenderDiff(diff string, colour bool) string {
	lines := strings.Split(diff, "\n")
	var sb strings.Builder
	for i, line := range lines {
		var rendered string
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			rendered = Colour(line, Bold, colour)
		case strings.HasPrefix(line, "@@"):
			rendered = Colour(line, Cyan, colour)
		case strings.HasPrefix(line, "+"):
			rendered = Colour(line, Green, colour)
		case strings.HasPrefix(line, "-"):
			rendered = Colour(line, Red, colour)
		default:
			rendered = line
		}
		sb.WriteString("  ");sb.WriteString(rendered)
		if i < len(lines)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// DiffStats returns a short stat line per file found in the diff.
// Format: "+12 -4  path/to/file.go"
func DiffStats(diff string, colour bool) []string {
	var results []string
	var currentFile string
	var additions, deletions int

	flush := func() {
		if currentFile == "" {
			return
		}
		add := fmt.Sprintf("+%d", additions)
		del := fmt.Sprintf("-%d", deletions)
		if colour {
			add = Colour(add, Green, true)
			del = Colour(del, Red, true)
		}
		results = append(results, fmt.Sprintf("%s %s  %s", add, del, currentFile))
		currentFile = ""
		additions = 0
		deletions = 0
	}

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			flush()
			path := strings.TrimPrefix(line, "+++ ")
			path = strings.TrimPrefix(path, "b/")
			if idx := strings.IndexByte(path, '\t'); idx != -1 {
				path = path[:idx]
			}
			currentFile = strings.TrimSpace(path)
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			additions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			deletions++
		}
	}
	flush()
	return results
}
