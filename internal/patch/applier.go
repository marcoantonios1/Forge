package patch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcoantonios1/Forge/internal/events"
)

type ApplyResult struct {
	Applied    []string
	Skipped    []string
	RolledBack []string
}

// Apply atomically applies a validated PatchSet to the repo at root.
// Panics if called with an unvalidated PatchSet — always call Validate first.
func Apply(root string, ps *PatchSet, emitter events.Emitter) (ApplyResult, error) {
	vr := Validate(root, ps)
	if !vr.Valid {
		msgs := make([]string, len(vr.Errors))
		for i, e := range vr.Errors {
			msgs[i] = e.Path + ": " + e.Message
		}
		panic("patch.Apply called on invalid PatchSet: " + strings.Join(msgs, "; "))
	}

	absRoot, _ := filepath.Abs(root)
	originals := make(map[string][]byte)
	var result ApplyResult

	paths := make([]string, 0, len(ps.Patches))
	for _, p := range ps.Patches {
		paths = append(paths, p.Path)
	}

	emitter.Emit(events.Event{
		Type:      events.EventFilePatchCreated,
		Timestamp: time.Now(),
		SessionID: ps.SessionID,
		Payload:   map[string]any{"session_id": ps.SessionID, "task_id": ps.TaskID, "files": paths},
	})

	for _, p := range ps.Patches {
		abs := filepath.Join(absRoot, p.Path)

		isNew := false
		var fileLines []string
		if p.IsNew {
			originals[p.Path] = nil
			isNew = true
			fileLines = []string{}
		} else {
			data, err := os.ReadFile(abs)
			if err != nil {
				return rollback(root, originals, result, emitter, ps.SessionID,
					fmt.Errorf("read %s: %w", p.Path, err))
			}
			originals[p.Path] = data
			fileLines = strings.Split(string(data), "\n")
		}

		patched, err := applyHunks(fileLines, p.Hunks)
		if err != nil {
			return rollback(root, originals, result, emitter, ps.SessionID,
				fmt.Errorf("apply hunks to %s: %w", p.Path, err))
		}

		if isNew {
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				return rollback(root, originals, result, emitter, ps.SessionID,
					fmt.Errorf("mkdir %s: %w", filepath.Dir(abs), err))
			}
		}

		perm := os.FileMode(0644)
		if !isNew {
			if info, err := os.Stat(abs); err == nil {
				perm = info.Mode()
			}
		}

		if err := os.WriteFile(abs, []byte(strings.Join(patched, "\n")), perm); err != nil {
			return rollback(root, originals, result, emitter, ps.SessionID,
				fmt.Errorf("write %s: %w", p.Path, err))
		}

		emitter.Emit(events.Event{
			Type:      events.EventFilePatchApplied,
			Timestamp: time.Now(),
			SessionID: ps.SessionID,
			Payload:   map[string]any{"path": p.Path, "hunks": len(p.Hunks)},
		})

		result.Applied = append(result.Applied, p.Path)
	}

	return result, nil
}

// applyHunks applies hunks in reverse order so line numbers stay valid.
func applyHunks(lines []string, hunks []Hunk) ([]string, error) {
	sorted := make([]Hunk, len(hunks))
	copy(sorted, hunks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].OldStart > sorted[j].OldStart
	})

	for _, h := range sorted {
		start := h.OldStart - 1 // 0-indexed

		var newLines []string
		for _, dl := range h.Lines {
			if len(dl) == 0 {
				continue
			}
			switch dl[0] {
			case '+':
				newLines = append(newLines, dl[1:])
			case '-':
				// removed — skip
			case ' ':
				newLines = append(newLines, dl[1:])
			}
		}

		end := start + h.OldLines
		if start < 0 || end > len(lines) {
			return nil, fmt.Errorf("hunk range [%d,%d) out of bounds (file has %d lines)", start, end, len(lines))
		}

		lines = append(lines[:start], append(newLines, lines[end:]...)...)
	}

	return lines, nil
}

func rollback(root string, originals map[string][]byte, result ApplyResult, emitter events.Emitter, sessionID string, cause error) (ApplyResult, error) {
	absRoot, _ := filepath.Abs(root)

	for path, data := range originals {
		abs := filepath.Join(absRoot, path)
		if data == nil {
			_ = os.Remove(abs)
			removeEmptyDirs(filepath.Dir(abs), absRoot)
		} else {
			_ = os.WriteFile(abs, data, 0644)
		}
		result.RolledBack = append(result.RolledBack, path)
		emitter.Emit(events.Event{
			Type:      events.EventFilePatchFailed,
			Timestamp: time.Now(),
			SessionID: sessionID,
			Payload:   map[string]any{"path": path, "error": cause.Error()},
		})
	}

	return result, cause
}

// removeEmptyDirs walks up from dir toward root, deleting empty directories.
func removeEmptyDirs(dir, root string) {
	for dir != root && strings.HasPrefix(dir, root) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
}
