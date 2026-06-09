package ui

import (
	"fmt"
	"strings"

	"github.com/marcoantonios1/Forge/internal/events"
)

const boxWidth = 42

// BuildSummary produces the task.completed block.
func BuildSummary(e events.Event, colour bool) string {
	summary := strOrEmpty(e.Payload, "summary")
	iterations := num(e.Payload, "iterations")
	files := extractStringSlice(e.Payload["files"])
	diff, _ := e.Payload["diff"].(string)
	tokens, hasTokens := intFromPayload(e.Payload, "tokens")

	var sb strings.Builder

	// Box.
	sb.WriteString(buildBox(colour))

	// Summary sentence.
	if summary != "" {
		sb.WriteString("   ");sb.WriteString(summary);sb.WriteString("\n")
	}

	// Files changed section.
	var stats []string
	if diff != "" {
		stats = DiffStats(diff, colour)
	} else if len(files) > 0 {
		for _, f := range files {
			stats = append(stats, Colour(f, Bold, colour))
		}
	}
	if len(stats) > 0 {
		sb.WriteString("\n")
		sb.WriteString("   Files changed:\n")
		g := glyph("✔", "+", colour)
		for _, s := range stats {
			sb.WriteString(fmt.Sprintf("     %s  %s\n", Colour(g, Green, colour), s))
		}
	}

	// Footer line.
	sb.WriteString("\n")
	if hasTokens {
		sb.WriteString(fmt.Sprintf("   Iterations: %d   Tokens used: %s\n",
			iterations, formatInt(tokens)))
	} else {
		sb.WriteString(fmt.Sprintf("   Iterations: %d\n", iterations))
	}

	return strings.TrimRight(sb.String(), "\n")
}

func buildBox(colour bool) string {
	var sb strings.Builder
	title := "  Task complete"
	inner := padRight(title, boxWidth)

	if colour {
		hline := strings.Repeat("═", boxWidth)
		top := Colour("╔"+hline+"╗", Bold+Green, colour)
		mid := Colour("║", Bold+Green, colour) + inner + Colour("║", Bold+Green, colour)
		bot := Colour("╚"+hline+"╝", Bold+Green, colour)
		sb.WriteString(top);sb.WriteString("\n");sb.WriteString(mid);sb.WriteString("\n");sb.WriteString(bot);sb.WriteString("\n")
	} else {
		hline := strings.Repeat("-", boxWidth)
		sb.WriteString("+");sb.WriteString(hline);sb.WriteString("+\n")
		sb.WriteString("|");sb.WriteString(inner);sb.WriteString("|\n")
		sb.WriteString("+");sb.WriteString(hline);sb.WriteString("+\n")
	}
	return sb.String()
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func strOrEmpty(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	return v
}

func intFromPayload(payload map[string]any, key string) (int, bool) {
	v, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
