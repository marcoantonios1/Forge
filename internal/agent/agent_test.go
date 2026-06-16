package agent

import "testing"

func TestStuckState_RepeatedToolCalls(t *testing.T) {
	s := newStuckState()
	args := map[string]any{"path": "main.go", "root": "/repo-a"}
	s.recordToolCall("read_file", args)
	s.recordToolCall("read_file", map[string]any{"path": "main.go", "root": "/repo-b"})
	s.recordToolCall("read_file", args)

	if reason := s.isStuck(3, 3); reason == "" {
		t.Fatal("expected stuck due to repeated identical tool call (root key should be ignored)")
	}
}

func TestStuckState_DifferentToolCallsNotStuck(t *testing.T) {
	s := newStuckState()
	s.recordToolCall("read_file", map[string]any{"path": "a.go"})
	s.recordToolCall("read_file", map[string]any{"path": "b.go"})
	s.recordToolCall("read_file", map[string]any{"path": "c.go"})

	if reason := s.isStuck(3, 3); reason != "" {
		t.Fatalf("expected not stuck, got reason: %q", reason)
	}
}

func TestStuckState_RepeatedResponses(t *testing.T) {
	s := newStuckState()
	s.recordResponse("FORGE_CLARIFY: which file?")
	s.recordResponse("FORGE_CLARIFY: which file?")

	if reason := s.isStuck(3, 3); reason == "" {
		t.Fatal("expected stuck due to repeated identical model response")
	}
}

func TestStuckState_RepeatedToolResults(t *testing.T) {
	s := newStuckState()
	result := map[string]any{"exit_code": 1, "stderr": "permission denied"}
	s.recordToolResult("run_command", result)
	s.recordToolResult("run_command", result)
	s.recordToolResult("run_command", result)

	if reason := s.isStuck(3, 3); reason == "" {
		t.Fatal("expected stuck due to tool returning identical result 3 times")
	}
}

func TestStuckState_BelowMinIterDoesNotFire(t *testing.T) {
	s := newStuckState()
	args := map[string]any{"path": "main.go"}
	s.recordToolCall("read_file", args)
	s.recordToolCall("read_file", args)
	s.recordToolCall("read_file", args)

	if reason := s.isStuck(2, 3); reason != "" {
		t.Fatalf("expected no stuck check below minIter, got reason: %q", reason)
	}
}

func TestStuckState_SlidingWindowKeepsOnlyRecent(t *testing.T) {
	s := newStuckState()
	s.recordToolCall("read_file", map[string]any{"path": "a.go"})
	s.recordToolCall("read_file", map[string]any{"path": "b.go"})
	s.recordToolCall("read_file", map[string]any{"path": "c.go"})
	s.recordToolCall("read_file", map[string]any{"path": "c.go"})
	s.recordToolCall("read_file", map[string]any{"path": "c.go"})

	if len(s.recentToolCalls) != 3 {
		t.Fatalf("expected window capped at 3, got %d", len(s.recentToolCalls))
	}
	if reason := s.isStuck(5, 3); reason == "" {
		t.Fatal("expected stuck once the oldest distinct call has rolled out of the window")
	}
}

func TestStuckState_NilResultFingerprint(t *testing.T) {
	s := newStuckState()
	s.recordToolResult("git_pull", nil)
	s.recordToolResult("git_pull", nil)
	s.recordToolResult("git_pull", nil)

	if reason := s.isStuck(3, 3); reason == "" {
		t.Fatal("expected nil results to fingerprint consistently as toolname:null and trigger stuck")
	}
}
