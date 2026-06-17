package memory

import "time"

const CurrentVersion = 1
const maxTaskHistory = 20
const maxMemoryBytes = 32 * 1024

type Conventions struct {
	Style  string   `json:"style,omitempty"`
	Banned []string `json:"banned,omitempty"`
	Build  string   `json:"build,omitempty"`
}

type TaskHistoryEntry struct {
	Summary   string    `json:"summary"`
	Files     []string  `json:"files"`
	Timestamp time.Time `json:"timestamp"`
}

type Memory struct {
	Version     int                `json:"version"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Conventions Conventions        `json:"conventions"`
	TaskHistory []TaskHistoryEntry `json:"task_history"`
}
