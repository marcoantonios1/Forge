package patch

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/marcoantonios1/Forge/internal/events"
)

var ErrNothingToUndo = errors.New("patch: nothing to undo")

type PatchHistory struct {
	records []*PatchRecord
}

func NewPatchHistory() *PatchHistory {
	return &PatchHistory{}
}

func (h *PatchHistory) Push(r *PatchRecord) {
	h.records = append(h.records, r)
}

func (h *PatchHistory) Peek() *PatchRecord {
	if len(h.records) == 0 {
		return nil
	}
	return h.records[len(h.records)-1]
}

func (h *PatchHistory) Pop() *PatchRecord {
	if len(h.records) == 0 {
		return nil
	}
	r := h.records[len(h.records)-1]
	h.records = h.records[:len(h.records)-1]
	return r
}

func (h *PatchHistory) Len() int {
	return len(h.records)
}

// AllRecords returns all records in the history, oldest first.
func (h *PatchHistory) AllRecords() []*PatchRecord {
	out := make([]*PatchRecord, len(h.records))
	copy(out, h.records)
	return out
}

// Undo reverts the most recent non-reverted PatchRecord.
func (h *PatchHistory) Undo(root string, emitter events.Emitter) error {
	// Find the most recent non-reverted record.
	var target *PatchRecord
	for i := len(h.records) - 1; i >= 0; i-- {
		if !h.records[i].Reverted {
			target = h.records[i]
			break
		}
	}
	if target == nil {
		return ErrNothingToUndo
	}

	absRoot, _ := filepath.Abs(root)

	for path, data := range target.Originals {
		abs := filepath.Join(absRoot, path)
		perm := os.FileMode(0644)
		if info, err := os.Stat(abs); err == nil {
			perm = info.Mode()
		}
		if err := os.WriteFile(abs, data, perm); err != nil {
			return err
		}
		emitter.Emit(events.Event{
			Type:      events.EventFilePatchReverted,
			Timestamp: time.Now(),
			SessionID: target.PatchSet.SessionID,
			Payload:   map[string]any{"path": path},
		})
	}

	target.Reverted = true
	return nil
}
