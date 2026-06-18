package mode

import "strings"

// SessionMode controls tool-permission and patch-confirmation behaviour for
// an entire Forge session, layered on top of per-task execution_policy.
type SessionMode string

const (
	ModeSafe       SessionMode = "safe"
	ModeBalanced   SessionMode = "balanced"
	ModeAutonomous SessionMode = "autonomous"
)

// Parse converts a --mode flag value into a SessionMode. Empty or unrecognised
// values default to ModeSafe.
func Parse(raw string) SessionMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "balanced":
		return ModeBalanced
	case "autonomous":
		return ModeAutonomous
	case "safe", "":
		return ModeSafe
	default:
		return ModeSafe
	}
}

// PreApprovedCategories returns the set of tool categories this mode
// auto-approves, to be merged with whatever --allowed-tools supplies.
//   - safe:       none
//   - balanced:   read, git_read
//   - autonomous: read, git_read, patch, git_write, run (effectively "all")
//
// TODO: balanced mode's categories could be made configurable via env/config
// rather than hard-coded here.
func (m SessionMode) PreApprovedCategories() map[string]bool {
	switch m {
	case ModeBalanced:
		return map[string]bool{"read": true, "git_read": true}
	case ModeAutonomous:
		return map[string]bool{
			"read":      true,
			"git_read":  true,
			"patch":     true,
			"git_write": true,
			"run":       true,
		}
	default:
		return map[string]bool{}
	}
}

// Interactive reports whether the PermissionGate should prompt for categories
// not pre-approved. autonomous is never interactive; safe and balanced still
// prompt for whatever this mode hasn't pre-approved.
func (m SessionMode) Interactive() bool {
	return m != ModeAutonomous
}

// AutoConfirmPatches reports whether patches should be applied without a
// confirmation prompt, independent of task.ExecutionPolicy.
func (m SessionMode) AutoConfirmPatches() bool {
	return m == ModeAutonomous
}

// String renders for the session header, e.g. "[balanced]".
func (m SessionMode) String() string {
	return string(m)
}
