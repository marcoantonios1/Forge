package session

import (
	"time"

	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/patch"
)

// SavedSession is the on-disk shape of a resumed session.
// Only the last session per repo is kept (last.json is overwritten on every save).
//
// TODO: consider persisting the compiled Task alongside RawInput if
// resume-time re-compilation drift becomes a problem in practice
// (e.g. after a Forge upgrade that changes compiler behaviour).
type SavedSession struct {
	SessionID string                `json:"session_id"`
	Repo      string                `json:"repo"`      // absolute cwd path — used as lookup key
	RawInput  string                `json:"raw_input"` // original task text; re-compiled on resume
	History   []costguard.Message   `json:"history"`   // capped to last 20 at save time
	Patches   []*patch.PatchRecord  `json:"patches"`   // full patch history (uncapped — needed for undo)
	Timestamp time.Time             `json:"timestamp"`
}
