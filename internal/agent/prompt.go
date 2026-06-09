package agent

import (
	"encoding/json"
	"fmt"

	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/projectconfig"
)

const agentSystemPrompt = `You are Forge, an autonomous software engineering agent.
You receive structured engineering tasks. You operate exclusively via tool calls.
You never produce prose explanations — only tool calls or one of the terminal signals.

Available tools:
  read_file, list_files, search_code, git_status, git_diff, git_log

To call a tool, emit exactly:
  TOOL: <name>
  ARGS: {"key": "value", ...}

To modify files, emit a patch block (and nothing else) in this exact format:
  FORGE_PATCH_BEGIN
  <unified diff — one or more files>
  FORGE_PATCH_END

To signal completion, emit exactly:
  FORGE_DONE: <one-sentence summary of what was accomplished>

To signal failure, emit exactly:
  FORGE_FAILED: <one-sentence reason>

Rules:
- Never emit free-form explanations outside of FORGE_DONE/FORGE_FAILED
- Never call tools and emit FORGE_DONE in the same response — finish tools first
- A FORGE_PATCH_BEGIN block must always be followed by FORGE_DONE in the next turn
- Prefer targeted reads (specific file + line range) over full-repo scans
- If the task execution_policy is "safe", describe what you would do but wait
  for FORGE_PATCH confirmation before applying`

// TODO: supervised clarification loop — if task.ExecutionPolicy == supervised,
// ask one follow-up question to resolve ambiguity before beginning execution.

func SystemMessage(cfg *projectconfig.ProjectConfig) costguard.Message {
	content := agentSystemPrompt
	if cfg != nil && !cfg.IsZero() {
		content = cfg.SystemPromptBlock() + "\n\n" + content
	}
	return costguard.Message{Role: "system", Content: content}
}

func TaskMessage(task *compiler.Task) costguard.Message {
	b, _ := json.MarshalIndent(task, "", "  ")
	return costguard.Message{
		Role:    "user",
		Content: "Task:\n" + string(b),
	}
}

func ToolResultMessage(toolName string, result any, err error) costguard.Message {
	var content string
	if err != nil {
		content = fmt.Sprintf("Tool error for %s: %s", toolName, err.Error())
	} else {
		b, _ := json.MarshalIndent(result, "", "  ")
		content = fmt.Sprintf("Tool result for %s:\n%s", toolName, string(b))
	}
	return costguard.Message{Role: "user", Content: content}
}

func GitContextMessage(status, diff, log string) costguard.Message {
	return costguard.Message{
		Role:    "user",
		Content: "Repository state at task start:\n" + status + "\n\n" + diff + "\n\n" + log,
	}
}
