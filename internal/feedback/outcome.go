// Package feedback ships per-task outcome telemetry to Costguard's
// /v1/feedback endpoint so model routing and cost decisions can be informed
// by real task success/failure signals. Entirely opt-in via
// FORGE_FEEDBACK_ENABLED — see PostOutcome.
package feedback

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// TaskOutcome is the payload sent to Costguard's POST /v1/feedback endpoint
// after every completed task. All fields use JSON snake_case to match the
// Costguard API spec.
type TaskOutcome struct {
	// Identification
	SessionID       string `json:"session_id"`
	TaskFingerprint string `json:"task_fingerprint"` // SHA256 of normalised task text
	Category        string `json:"category"`         // from compiler.Task.Category
	Scope           string `json:"scope"`             // from compiler.Task.Scope

	// Outcome
	Status       string `json:"status"` // "completed" | "failed" | "rejected" | "cancelled"
	Summary      string `json:"summary"` // ac.LastSummary on completion; error text on failure
	FilesChanged int    `json:"files_changed"` // non-reverted patched files count
	Iterations   int    `json:"iterations"`

	// Quality signals
	ReviewRetries int  `json:"reviewer_retries"` // how many times the reviewer rejected before accepting
	UserAccepted  bool `json:"user_accepted"`     // true if patch was confirmed (vs auto-applied or rejected)

	// Cost signals
	TotalTokensUsed int   `json:"total_tokens_used"` // sum across all Costguard calls in this task
	DurationMs      int64 `json:"duration_ms"`

	// Routing metadata
	PlannerModel string `json:"planner_model"`
	CoderModel   string `json:"coder_model"`

	// Timestamp
	Timestamp time.Time `json:"timestamp"`
}

var wsCollapser = regexp.MustCompile(`\s+`)

// Fingerprint returns a SHA256 hex digest of the normalised task description:
// lowercased, whitespace collapsed to single spaces, trimmed. This gives
// Costguard a way to correlate similar tasks across sessions without storing
// raw task text in the feedback payload.
func Fingerprint(rawInput string) string {
	normalised := wsCollapser.ReplaceAllString(strings.ToLower(strings.TrimSpace(rawInput)), " ")
	h := sha256.Sum256([]byte(normalised))
	return hex.EncodeToString(h[:])
}
