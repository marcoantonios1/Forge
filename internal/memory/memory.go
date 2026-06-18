package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const memoryDir  = ".forge/memory"
const memoryFile = "memory.json"

func memoryPath(root string) string {
	return filepath.Join(root, memoryDir, memoryFile)
}

// Load reads memory.json from root. Returns a zero-value *Memory (not nil, not
// an error) when the file does not exist — absence is the normal first-run state.
func Load(root string) (*Memory, error) {
	path := memoryPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Memory{Version: CurrentVersion, TaskHistory: []TaskHistoryEntry{}}, nil
		}
		return nil, fmt.Errorf("memory: reading %s: %w", path, err)
	}
	var m Memory
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("memory: parsing %s: %w", path, err)
	}
	if m.Version == 0 {
		m.Version = CurrentVersion
	}
	// TODO: handle version 2+ migration logic here when CurrentVersion increments
	// and the schema changes in a backwards-incompatible way.
	return &m, nil
}

// Save writes m to root/.forge/memory/memory.json, creating directories as needed.
func Save(root string, m *Memory) error {
	m.enforceCaps()
	m.UpdatedAt = time.Now()

	path := memoryPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	return nil
}

// Clear deletes memory.json if it exists. Returns nil if it didn't exist.
func Clear(root string) error {
	path := memoryPath(root)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory: clear: %w", err)
	}
	return nil
}

// Update appends a new task_history entry and merges any non-empty convention
// fields. Empty string / nil values mean "don't overwrite".
//
// TODO: populate Conventions by asking the planner to emit a structured
// "CONVENTIONS_LEARNED: {...}" block at FORGE_DONE time, then parse it here.
func (m *Memory) Update(summary string, files []string, conventions Conventions) {
	m.TaskHistory = append(m.TaskHistory, TaskHistoryEntry{
		Summary:   summary,
		Files:     files,
		Timestamp: time.Now(),
	})
	if conventions.Style != "" {
		m.Conventions.Style = conventions.Style
	}
	if conventions.Build != "" {
		m.Conventions.Build = conventions.Build
	}
	if len(conventions.Banned) > 0 {
		m.Conventions.Banned = mergeUnique(m.Conventions.Banned, conventions.Banned)
	}
	m.enforceCaps()
}

func mergeUnique(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing))
	out := append([]string{}, existing...)
	for _, e := range existing {
		seen[e] = true
	}
	for _, n := range incoming {
		if !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	return out
}

// enforceCaps trims task_history to maxTaskHistory entries, then if the
// marshaled size still exceeds maxMemoryBytes it drops the oldest entries one
// at a time until it fits.
func (m *Memory) enforceCaps() {
	if len(m.TaskHistory) > maxTaskHistory {
		m.TaskHistory = m.TaskHistory[len(m.TaskHistory)-maxTaskHistory:]
	}
	for {
		b, err := json.Marshal(m)
		if err != nil || len(b) <= maxMemoryBytes {
			return
		}
		if len(m.TaskHistory) == 0 {
			return // conventions alone exceed cap — leave as-is rather than truncating fields
		}
		m.TaskHistory = m.TaskHistory[1:] // drop oldest
	}
}

func (c Conventions) isEmpty() bool {
	return c.Style == "" && c.Build == "" && len(c.Banned) == 0
}

// Inject returns the system-prompt block for this memory, or "" when there is
// nothing worth injecting (no conventions and no task history).
func (m *Memory) Inject() string {
	if m == nil || (m.Conventions.isEmpty() && len(m.TaskHistory) == 0) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<persistent_memory>\n")
	sb.WriteString("This is inferred context from previous sessions in this repo. ")
	sb.WriteString("If anything here conflicts with forge.md, forge.md always takes precedence.\n\n")

	if !m.Conventions.isEmpty() {
		sb.WriteString("Conventions:\n")
		if m.Conventions.Style != "" {
			fmt.Fprintf(&sb, "  style: %s\n", m.Conventions.Style)
		}
		if m.Conventions.Build != "" {
			fmt.Fprintf(&sb, "  build: %s\n", m.Conventions.Build)
		}
		if len(m.Conventions.Banned) > 0 {
			fmt.Fprintf(&sb, "  banned: %s\n", strings.Join(m.Conventions.Banned, ", "))
		}
		sb.WriteString("\n")
	}

	if len(m.TaskHistory) > 0 {
		sb.WriteString("Recent task history (most recent last):\n")
		for _, h := range m.TaskHistory {
			fmt.Fprintf(&sb, "  - [%s] %s (files: %s)\n",
				h.Timestamp.Format("2006-01-02"), h.Summary, strings.Join(h.Files, ", "))
		}
	}

	sb.WriteString("</persistent_memory>")
	return sb.String()
}
