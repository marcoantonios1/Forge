package compiler

import (
	"encoding/json"
	"errors"
	"strings"
)

const systemPrompt = `You are a task classification engine for an autonomous engineering agent called Forge.
Your only job is to classify the user's input and output a single JSON object. No explanations, no markdown fences, no preamble.

Forge can read code, answer questions about the repo, explain architecture, list files, summarise modules,
and perform any analysis task — not just write or modify code. Treat questions and inquiries about the
codebase as valid engineering tasks with category "analysis".

Always classify the input — never refuse it. If the input is vague, short, or doesn't obviously relate to
code or this repo, do your best to interpret it as a repo-wide analysis task and set execution_policy to
"supervised" so the agent can ask one clarifying question before doing any work, instead of guessing.

Output exactly this JSON schema with all fields populated:
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
- analysis → questions, explanations, summaries, investigations, or anything vague/ambiguous about the codebase
- feature  → adding new functionality
- bugfix   → fixing a defect
- refactor → restructuring without behaviour change
- infra    → CI, build, deployment, configuration changes

Rules for execution_policy:
- autonomous  → task is clear, self-contained, and low-risk (includes most analysis tasks)
- supervised  → task is ambiguous, vague, broad, touches multiple modules, or isn't clearly about code —
                this lets the agent ask one clarifying question before starting instead of guessing
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

// matchingBrace returns the index of the closing '}' that matches the '{' at
// position start, or -1 if none is found. Handles nested objects and strings.
func matchingBrace(s string, start int) int {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
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
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractJSON returns the first valid JSON object found in raw, stripping
// markdown fences and XML-style thinking blocks first. It tries every '{' in
// the cleaned string so that stray {…} in prose before the real object are
// skipped.
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

	// Remove XML-style blocks (e.g. <thinking>…</thinking>) so that {…}
	// inside them cannot produce false candidates.
	s = stripXMLBlocks(s)

	// Try every '{' as a potential object start and return the first candidate
	// that is valid JSON. This skips stray {…} in surrounding prose.
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := matchingBrace(s, i)
		if end == -1 {
			continue
		}
		candidate := s[i : end+1]
		if json.Valid([]byte(candidate)) {
			return candidate, nil
		}
	}

	return "", errors.New("compiler: no JSON object found in LLM response")
}
