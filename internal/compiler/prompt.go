package compiler

import (
	"errors"
	"strings"
)

const systemPrompt = `You are a task classification engine for an autonomous engineering agent called Forge.
Your only job is to classify the user's input and output a single JSON object. No explanations, no markdown fences, no preamble.

Forge can read code, answer questions about the repo, explain architecture, list files, summarise modules,
and perform any analysis task — not just write or modify code. Treat questions and inquiries about the
codebase as valid engineering tasks with category "analysis".

Only reject input that has NOTHING to do with software or this codebase — e.g. "what's the weather?",
"write me a poem", "who won the game last night". Output exactly:
{"rejected": true, "reason": "<one sentence explaining why>"}

If the input is related to code, the repo, software engineering, or the project in any way, output exactly
this JSON schema with all fields populated:
{
  "type": "engineering_task",
  "category": "<feature|bugfix|refactor|infra|analysis>",
  "scope": "<repo-wide|module|file-specific>",
  "constraints": [],
  "deliverables": [],
  "execution_policy": "<autonomous|supervised|safe>",
  "priority": "<normal|high|critical>"
}

Rules for category:
- analysis → questions, explanations, summaries, or investigations about the codebase
- feature  → adding new functionality
- bugfix   → fixing a defect
- refactor → restructuring without behaviour change
- infra    → CI, build, deployment, configuration changes

Rules for execution_policy:
- autonomous  → task is clear, self-contained, and low-risk (includes most analysis tasks)
- supervised  → task is ambiguous, broad, or touches multiple modules
- safe        → task involves deletion, secrets, infrastructure changes, or is otherwise high-risk

Rules for priority:
- critical → input contains "urgent", "production", "outage", "broken", or "ASAP"
- high     → input contains "important", "blocking", or "release"
- normal   → everything else

Output only the JSON object. Nothing else.`

// stripXMLBlocks removes XML-style tag pairs (e.g. <thinking>…</thinking>)
// that some models prepend to their output. Unmatched or self-closing tags are
// deleted in place. Operates in a loop so nested blocks are also removed.
func stripXMLBlocks(s string) string {
	for {
		lt := strings.Index(s, "<")
		if lt == -1 {
			break
		}
		gt := strings.Index(s[lt:], ">")
		if gt == -1 {
			break
		}
		inner := strings.TrimSpace(s[lt+1 : lt+gt])
		// Skip closing tags, self-closing tags, declarations, and processing instructions.
		if inner == "" || inner[0] == '/' || inner[0] == '?' || inner[0] == '!' || inner[len(inner)-1] == '/' {
			s = s[:lt] + s[lt+gt+1:]
			continue
		}
		// Extract the tag name (first word, ignoring attributes).
		tagName := inner
		if sp := strings.IndexAny(inner, " \t\r\n"); sp != -1 {
			tagName = inner[:sp]
		}
		closing := "</" + tagName + ">"
		ci := strings.Index(s[lt:], closing)
		if ci == -1 {
			// No matching closing tag — remove just the opening tag and move on.
			s = s[:lt] + s[lt+gt+1:]
			continue
		}
		s = s[:lt] + s[lt+ci+len(closing):]
	}
	return strings.TrimSpace(s)
}

// extractJSON returns the first complete JSON object found in raw, stripping
// markdown fences and XML-style thinking blocks before applying bracket-depth
// tracking to locate the object boundaries precisely.
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

	// Remove XML-style blocks (e.g. <thinking>…</thinking>) so that any
	// {…} inside them does not confuse the bracket tracker below.
	s = stripXMLBlocks(s)

	// Walk the string tracking bracket depth and string context so we return
	// exactly the first complete {...} object, ignoring anything before or after.
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start != -1 {
				return s[start : i+1], nil
			}
		}
	}

	return "", errors.New("compiler: no JSON object found in LLM response")
}
