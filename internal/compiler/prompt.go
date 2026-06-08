package compiler

import (
	"errors"
	"strings"
)

const systemPrompt = `You are a task classification engine for an autonomous engineering agent called Forge.
Your only job is to classify the user's input and output a single JSON object. No explanations, no markdown fences, no preamble.

If the input is NOT an engineering task (e.g. general questions, conversation, nonsense), output exactly:
{"rejected": true, "reason": "<one sentence explaining why>"}

If the input IS an engineering task, output exactly this JSON schema with all fields populated:
{
  "type": "engineering_task",
  "category": "<feature|bugfix|refactor|infra|analysis>",
  "scope": "<repo-wide|module|file-specific>",
  "constraints": [],
  "deliverables": [],
  "execution_policy": "<autonomous|supervised|safe>",
  "priority": "<normal|high|critical>"
}

Rules for execution_policy:
- autonomous  → task is clear, self-contained, and low-risk
- supervised  → task is ambiguous, broad, or touches multiple modules
- safe        → task involves deletion, secrets, infrastructure changes, or is otherwise high-risk

Rules for priority:
- critical → input contains "urgent", "production", "outage", "broken", or "ASAP"
- high     → input contains "important", "blocking", or "release"
- normal   → everything else

Output only the JSON object. Nothing else.`

// extractJSON returns the first JSON object found in raw, stripping any
// accidental markdown fences or surrounding whitespace.
func extractJSON(raw string) (string, error) {
	s := strings.TrimSpace(raw)

	// Strip markdown fences if present.
	if strings.HasPrefix(s, "```") {
		end := strings.LastIndex(s, "```")
		if end > 3 {
			s = strings.TrimSpace(s[3:end])
			// Strip optional language tag on opening fence line.
			if nl := strings.Index(s, "\n"); nl != -1 {
				first := strings.TrimSpace(s[:nl])
				if !strings.Contains(first, "{") {
					s = strings.TrimSpace(s[nl+1:])
				}
			}
		}
	}

	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return "", errors.New("compiler: no JSON object found in LLM response")
	}
	return s[start : end+1], nil
}
