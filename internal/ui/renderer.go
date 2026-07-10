package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/marcoantonios1/Forge/internal/events"
)

// TODO: --output=json flag would force ModeDebug for all output.

type Mode int

const (
	ModeHuman Mode = iota
	ModeDebug
	ModePlain
)

type Renderer struct {
	mode   Mode
	colour bool
	out    io.Writer
	mu     sync.Mutex
}

// New creates a Renderer. ModeHuman auto-detects colour from IsTTY(os.Stdout).
// If not a TTY, mode is downgraded to ModePlain.
func New(out io.Writer, mode Mode) *Renderer {
	colour := false
	if mode == ModeHuman {
		if f, ok := out.(*os.File); ok {
			colour = IsTTY(f)
		}
		if !colour {
			mode = ModePlain
		}
	}
	return &Renderer{mode: mode, colour: colour, out: out}
}

// Emit implements events.Emitter. Safe for concurrent use.
func (r *Renderer) Emit(e events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.mode == ModeDebug {
		b, _ := json.Marshal(e)
		fmt.Fprintf(r.out, "%s\n", b)
		return
	}

	// TODO: live-updating spinner for long-running tool calls would hook in here.

	var line string
	switch e.Type {
	case events.EventTaskStarted:
		line = formatTaskStarted(e, r.colour)
	case events.EventToolInvoked:
		line = formatToolInvoked(e, r.colour)
	case events.EventToolOutput:
		line = formatToolOutput(e, r.colour)
	case events.EventPatchReviewed:
		line = formatPatchReviewed(e, r.colour)
	case events.EventFilePatchCreated:
		line = formatFilePatchCreated(e, r.colour)
	case events.EventFilePatchApplied:
		line = formatFilePatchApplied(e, r.colour)
	case events.EventFilePatchReverted:
		line = formatFilePatchReverted(e, r.colour)
	case events.EventFilePatchFailed:
		line = formatFilePatchFailed(e, r.colour)
	case events.EventTaskComplete:
		line = BuildSummary(e, r.colour)
	case events.EventClarificationAsked:
		line = formatClarificationAsked(e, r.colour)
	case events.EventClarificationAnswered:
		line = formatClarificationAnswered(e, r.colour)
	case events.EventGitBranch:
		line = formatGitBranch(e, r.colour)
	case events.EventGitCommit:
		line = formatGitCommit(e, r.colour)
	case events.EventGitPush:
		line = formatGitPush(e, r.colour)
	case events.EventGitStash:
		line = formatGitStash(e, r.colour)
	case events.EventCommandStarted:
		line = formatCommandStarted(e, r.colour)
	case events.EventCommandOutput:
		line = formatCommandOutput(e, r.colour)
	case events.EventCommandFinished:
		line = formatCommandFinished(e, r.colour)
	case events.EventTaskFailed:
		line = formatTaskFailed(e, r.colour)
	case events.EventMCPConnected:
		line = formatMCPConnected(e, r.colour)
	case events.EventMCPError:
		line = formatMCPError(e, r.colour)
	case events.EventModelEscalated:
		line = formatModelEscalated(e, r.colour)
	case events.EventConfirmDecision:
		decision, _ := e.Payload["decision"].(string)
		files := extractStringSlice(e.Payload["files"])
		switch decision {
		case "approved":
			line = fmt.Sprintf("  %s  patch approved  (%d file(s))",
				glyph("✔", "+", r.colour), len(files))
			line = Colour(line, Green, r.colour)
		case "approved-all":
			line = fmt.Sprintf("  %s  approved all patches for session",
				glyph("✔", "+", r.colour))
			line = Colour(line, Green, r.colour)
		case "declined":
			line = fmt.Sprintf("  %s  patch declined",
				glyph("✘", "x", r.colour))
			line = Colour(line, Yellow, r.colour)
		default:
			return
		}
	default:
		return // skip unknown events silently in human/plain mode
	}

	fmt.Fprintf(r.out, "%s\n", line)
}
