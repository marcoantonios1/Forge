package confirm

import (
	"fmt"
	"strings"

	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/ui"
)

const previewWidth = 50

// RenderPreview formats a PatchSet for display before the confirmation prompt.
// Reconstructs diff text from Hunk data — does not read the filesystem.
func RenderPreview(ps *patch.PatchSet, colour bool) string {
	var sb strings.Builder

	sb.WriteString(buildPreviewBox(len(ps.Patches), colour))
	sb.WriteByte('\n')

	for i, p := range ps.Patches {
		header := fmt.Sprintf("File %d/%d: %s", i+1, len(ps.Patches), p.Path)
		divider := strings.Repeat("─", max(len(header), 34))

		if colour {
			sb.WriteString(ui.Colour(header, ui.Bold, true));sb.WriteString("\n")
			sb.WriteString(ui.Colour(divider, ui.Dim, true));sb.WriteString("\n")
		} else {
			sb.WriteString(header);sb.WriteString("\n")
			sb.WriteString(divider);sb.WriteString("\n")
		}

		diff := reconstructDiff(p)
		sb.WriteString(ui.RenderDiff(diff, colour))
		sb.WriteString("\n\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

func buildPreviewBox(fileCount int, colour bool) string {
	body := fmt.Sprintf("  %d file(s) will be modified", fileCount)
	inner := padRight(body, previewWidth)

	if colour {
		hline := strings.Repeat("─", previewWidth)
		top := ui.Colour("┌─ Patch preview "+strings.Repeat("─", previewWidth-len("─ Patch preview "))+"┐", ui.Cyan, true)
		mid := ui.Colour("│", ui.Cyan, true) + inner + ui.Colour("│", ui.Cyan, true)
		bot := ui.Colour("└"+hline+"┘", ui.Cyan, true)
		return top + "\n" + mid + "\n" + bot
	}
	hline := strings.Repeat("-", previewWidth)
	return "+-" + "Patch preview" + strings.Repeat("-", previewWidth-len("Patch preview")-1) + "+\n" +
		"|" + inner + "|\n" +
		"+" + hline + "+"
}

// reconstructDiff rebuilds unified diff text from Patch.Hunks.
func reconstructDiff(p patch.Patch) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n", p.Path)
	fmt.Fprintf(&sb, "+++ b/%s\n", p.Path)
	for _, h := range p.Hunks {
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
		for _, line := range h.Lines {
			sb.WriteString(line);sb.WriteString("\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
