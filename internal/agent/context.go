package agent

import (
	"time"

	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/patch"
	"github.com/marcoantonios1/Forge/internal/projectconfig"
)

type AgentContext struct {
	SessionID     string
	Task          *compiler.Task
	Root          string
	ProjectConfig *projectconfig.ProjectConfig
	History       []costguard.Message
	Patches       *patch.PatchHistory
	Iteration     int
	StartedAt     time.Time
	LastSummary   string // populated by agent.Run() on FORGE_DONE
}

func NewAgentContext(
	sessionID string,
	task *compiler.Task,
	root string,
	cfg *projectconfig.ProjectConfig,
	history *patch.PatchHistory,
) *AgentContext {
	return &AgentContext{
		SessionID:     sessionID,
		Task:          task,
		Root:          root,
		ProjectConfig: cfg,
		History:       []costguard.Message{},
		Patches:       history,
		StartedAt:     time.Now(),
	}
}
