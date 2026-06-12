package patch

import "time"

type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []string
}

type Patch struct {
	Path  string
	Hunks []Hunk
	IsNew bool // true when the patch creates a file that does not yet exist
}

type PatchSet struct {
	SessionID string
	TaskID    string
	Patches   []Patch
	CreatedAt time.Time
}

type PatchRecord struct {
	PatchSet  PatchSet
	Originals map[string][]byte
	AppliedAt time.Time
	Reverted  bool
}
