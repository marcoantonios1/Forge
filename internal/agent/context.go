package agent

import (
	"time"

	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/memory"
	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/projectconfig"
)

type AgentContext struct {
	SessionID     string
	Task          *compiler.Task
	Root          string
	ProjectConfig *projectconfig.ProjectConfig
	Memory        *memory.Memory // loaded at session start; nil = memory disabled/unavailable
	History       []costguard.Message
	Patches       *patch.PatchHistory
	Iteration     int
	StartedAt     time.Time
	LastSummary   string // populated by agent.Run() on FORGE_DONE
	AppliedBranch      string // branch created for this task (set post-task)
	AppliedCommit      string // short commit hash (set post-task)
	ClarificationAsked bool   // true after clarify() runs; prevents a second round
}

func NewAgentContext(
	sessionID string,
	task *compiler.Task,
	root string,
	cfg *projectconfig.ProjectConfig,
	history *patch.PatchHistory,
	mem *memory.Memory,
) *AgentContext {
	return &AgentContext{
		SessionID:     sessionID,
		Task:          task,
		Root:          root,
		ProjectConfig: cfg,
		Memory:        mem,
		History:       []costguard.Message{},
		Patches:       history,
		StartedAt:     time.Now(),
	}
}
