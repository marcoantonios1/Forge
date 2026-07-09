package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/config"
	"github.com/marcoantonios1/Forge/internal/confirm"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/feedback"
	"github.com/marcoantonios1/Forge/internal/patch"
)

type noopEmitter struct{}

func (noopEmitter) Emit(events.Event) {}

// newTestChatServer returns an httptest.Server that answers every
// /v1/chat/completions call with content and a fixed total_tokens usage.
func newTestChatServer(t *testing.T, content string, totalTokens int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := costguard.ChatResponse{
			ID: "test",
			Choices: []costguard.Choice{
				{Message: costguard.Message{Role: "assistant", Content: content}},
			},
			Usage: costguard.Usage{TotalTokens: totalTokens},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
}

func newTestAgent(t *testing.T, chatURL, feedbackURL string) (*Agent, *AgentContext) {
	t.Helper()
	cfg := &config.Config{CostguardURL: chatURL, Timeout: 5 * time.Second, MaxRetries: 0}
	client := costguard.New(cfg)

	registry := NewRegistry(t.TempDir(), noopEmitter{}, "sess1", nil, nil, nil, nil)

	agentCfg := Config{
		PlannerModel:    "test-planner",
		CoderModel:      "test-coder",
		FeedbackEnabled: feedbackURL != "",
		FeedbackBaseURL: feedbackURL,
	}
	ag := New(agentCfg, client, registry, noopEmitter{}, confirm.AutoConfirmer{}, nil, nil, nil, nil)

	task := &compiler.Task{
		Category:        compiler.CategoryBugfix,
		Scope:           compiler.ScopeFileSpecific,
		ExecutionPolicy: compiler.PolicyAutonomous,
		RawInput:        "test task",
	}
	ac := NewAgentContext("sess1", task, t.TempDir(), nil, patch.NewPatchHistory(), nil)
	return ag, ac
}

func TestRunAccumulatesTokensAndPostsCompletedOutcome(t *testing.T) {
	chatSrv := newTestChatServer(t, "FORGE_DONE: did the thing", 42)
	defer chatSrv.Close()

	received := make(chan feedback.TaskOutcome, 1)
	feedbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var outcome feedback.TaskOutcome
		json.NewDecoder(r.Body).Decode(&outcome) //nolint:errcheck
		received <- outcome
	}))
	defer feedbackSrv.Close()

	ag, ac := newTestAgent(t, chatSrv.URL, feedbackSrv.URL)

	if err := ag.Run(context.Background(), ac); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ac.TotalTokensUsed != 42 {
		t.Fatalf("TotalTokensUsed = %d, want 42", ac.TotalTokensUsed)
	}

	select {
	case outcome := <-received:
		if outcome.Status != "completed" {
			t.Errorf("Status = %q, want completed", outcome.Status)
		}
		if outcome.TotalTokensUsed != 42 {
			t.Errorf("TotalTokensUsed = %d, want 42", outcome.TotalTokensUsed)
		}
		if outcome.TaskFingerprint != feedback.Fingerprint("test task") {
			t.Errorf("TaskFingerprint mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feedback POST")
	}
}

func TestRunPostsFailedOutcomeOnceOnForgeFailed(t *testing.T) {
	chatSrv := newTestChatServer(t, "FORGE_FAILED: could not do the thing", 7)
	defer chatSrv.Close()

	var postCount int
	received := make(chan feedback.TaskOutcome, 4)
	feedbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount++
		var outcome feedback.TaskOutcome
		json.NewDecoder(r.Body).Decode(&outcome) //nolint:errcheck
		received <- outcome
	}))
	defer feedbackSrv.Close()

	ag, ac := newTestAgent(t, chatSrv.URL, feedbackSrv.URL)

	runErr := ag.Run(context.Background(), ac)
	var taskFailed *ErrTaskFailed
	if !errors.As(runErr, &taskFailed) {
		t.Fatalf("expected *ErrTaskFailed, got %v (%T)", runErr, runErr)
	}

	// Mirrors cmd/forge/main.go's double-post guard: only call PostTaskError
	// when the error is NOT an *ErrTaskFailed (whose outcome Run() already posted).
	if !errors.As(runErr, &taskFailed) {
		ag.PostTaskError(ac, runErr)
	}

	select {
	case outcome := <-received:
		if outcome.Status != "failed" {
			t.Errorf("Status = %q, want failed", outcome.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feedback POST")
	}

	// Give any accidental second POST a chance to arrive before asserting count.
	time.Sleep(200 * time.Millisecond)
	if postCount != 1 {
		t.Fatalf("feedback endpoint hit %d times, want exactly 1 (no double-post for FORGE_FAILED)", postCount)
	}
}

func TestRunDoesNotPostWhenFeedbackDisabled(t *testing.T) {
	chatSrv := newTestChatServer(t, "FORGE_DONE: did the thing", 5)
	defer chatSrv.Close()

	var postCount int
	feedbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postCount++
	}))
	defer feedbackSrv.Close()

	// feedbackURL="" -> FeedbackEnabled=false in newTestAgent.
	ag, ac := newTestAgent(t, chatSrv.URL, "")
	// Still point FeedbackBaseURL at the server to prove disabling, not a bad URL, is what stops the call.
	ag.feedbackBaseURL = feedbackSrv.URL

	if err := ag.Run(context.Background(), ac); err != nil {
		t.Fatalf("Run: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if postCount != 0 {
		t.Fatalf("feedback endpoint hit %d times, want 0 when FeedbackEnabled=false", postCount)
	}
}
