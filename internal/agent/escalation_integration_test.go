package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/Forge/internal/compiler"
	"github.com/marcoantonios1/Forge/internal/config"
	"github.com/marcoantonios1/Forge/internal/confirm"
	"github.com/marcoantonios1/Forge/internal/costguard"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/patch"
)

// recordingEmitter captures every event it receives, for assertions on
// ModelEscalatedEvent payloads without needing a real renderer.
type recordingEmitter struct {
	mu     sync.Mutex
	events []events.Event
}

func (e *recordingEmitter) Emit(ev events.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}

func (e *recordingEmitter) escalations() []events.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []events.Event
	for _, ev := range e.events {
		if ev.Type == events.EventModelEscalated {
			out = append(out, ev)
		}
	}
	return out
}

// newStatsServer returns an httptest.Server answering GET /v1/feedback/stats
// with a fixed reviewer_pass_rate, and a counter of how many times it was hit.
func newStatsServer(passRate float64) (*httptest.Server, *int32) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"reviewer_pass_rate": passRate,
		})
	}))
	return srv, &hits
}

func newEscalationTestAgent(t *testing.T, emitter events.Emitter, agentCfg Config) (*Agent, *AgentContext) {
	t.Helper()
	cfg := &config.Config{CostguardURL: "http://127.0.0.1:0", Timeout: 5 * time.Second}
	client := costguard.New(cfg)
	registry := NewRegistry(t.TempDir(), emitter, "sess1", nil, nil, nil, nil)
	ag := New(agentCfg, client, registry, emitter, confirm.AutoConfirmer{}, nil, nil, nil, nil)

	task := &compiler.Task{
		Category:        compiler.CategoryBugfix,
		Scope:           compiler.ScopeFileSpecific,
		ExecutionPolicy: compiler.PolicyAutonomous,
		RawInput:        "test task",
	}
	ac := NewAgentContext("sess1", task, t.TempDir(), nil, patch.NewPatchHistory(), nil)
	return ag, ac
}

func TestFetchAndEscalatePreEmptiveBelowThreshold(t *testing.T) {
	statsSrv, hits := newStatsServer(0.30) // below default 0.60 threshold
	defer statsSrv.Close()

	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		PlannerModel:         "primary-planner",
		FeedbackEnabled:      true,
		FeedbackBaseURL:      statsSrv.URL,
		PlannerFallbackModel: "fallback-planner",
	})

	ag.fetchAndEscalate(context.Background(), ac)

	if *hits == 0 {
		t.Fatal("expected fetchAndEscalate to hit the stats endpoint")
	}
	if got := ag.selectActiveModel(RolePlanner); got != "fallback-planner" {
		t.Fatalf("selectActiveModel(RolePlanner) = %q, want fallback-planner", got)
	}

	esc := emitter.escalations()
	if len(esc) != 1 {
		t.Fatalf("expected 1 ModelEscalatedEvent, got %d", len(esc))
	}
	if esc[0].Payload["reason"] != "low_pass_rate" {
		t.Fatalf("reason = %v, want low_pass_rate", esc[0].Payload["reason"])
	}
	if esc[0].Payload["from"] != "primary-planner" || esc[0].Payload["to"] != "fallback-planner" {
		t.Fatalf("unexpected from/to: %+v", esc[0].Payload)
	}
}

func TestFetchAndEscalateAboveThresholdNoEscalation(t *testing.T) {
	statsSrv, _ := newStatsServer(0.95) // above threshold
	defer statsSrv.Close()

	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		PlannerModel:         "primary-planner",
		FeedbackEnabled:      true,
		FeedbackBaseURL:      statsSrv.URL,
		PlannerFallbackModel: "fallback-planner",
	})

	ag.fetchAndEscalate(context.Background(), ac)

	if got := ag.selectActiveModel(RolePlanner); got != "primary-planner" {
		t.Fatalf("selectActiveModel(RolePlanner) = %q, want primary-planner (no escalation)", got)
	}
	if len(emitter.escalations()) != 0 {
		t.Fatalf("expected no escalation events, got %d", len(emitter.escalations()))
	}
}

func TestFetchAndEscalateDisabledWhenFeedbackOff(t *testing.T) {
	statsSrv, hits := newStatsServer(0.0) // would trigger escalation if reached
	defer statsSrv.Close()

	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		PlannerModel:         "primary-planner",
		FeedbackEnabled:      false, // <-- disabled
		FeedbackBaseURL:      statsSrv.URL,
		PlannerFallbackModel: "fallback-planner",
	})

	ag.fetchAndEscalate(context.Background(), ac)

	if *hits != 0 {
		t.Fatalf("expected no HTTP call when FeedbackEnabled=false, got %d hits", *hits)
	}
	if got := ag.selectActiveModel(RolePlanner); got != "primary-planner" {
		t.Fatalf("selectActiveModel(RolePlanner) = %q, want primary-planner", got)
	}
}

func TestFetchAndEscalateNoFallbackConfiguredSkipsFetch(t *testing.T) {
	statsSrv, hits := newStatsServer(0.0)
	defer statsSrv.Close()

	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		PlannerModel:    "primary-planner",
		FeedbackEnabled: true,
		FeedbackBaseURL: statsSrv.URL,
		// no fallback models configured for any role
	})

	ag.fetchAndEscalate(context.Background(), ac)

	if *hits != 0 {
		t.Fatalf("expected no HTTP call when no fallback models configured, got %d hits", *hits)
	}
}

func TestFetchAndEscalateUnreachableStatsServerIsSafe(t *testing.T) {
	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		PlannerModel:         "primary-planner",
		FeedbackEnabled:      true,
		FeedbackBaseURL:      "http://127.0.0.1:1", // nothing listening
		PlannerFallbackModel: "fallback-planner",
	})

	// Must not panic or hang — fetchPassRate returns (0, false) and
	// fetchAndEscalate just skips escalation for that role.
	ag.fetchAndEscalate(context.Background(), ac)

	if got := ag.selectActiveModel(RolePlanner); got != "primary-planner" {
		t.Fatalf("selectActiveModel(RolePlanner) = %q, want primary-planner (unreachable stats = no escalation)", got)
	}
	if len(emitter.escalations()) != 0 {
		t.Fatalf("expected no escalation events when stats server unreachable")
	}
}

func TestEscalateRoleNoopWithoutFallback(t *testing.T) {
	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		CoderModel: "primary-coder",
		// no CoderFallbackModel configured
	})

	ag.escalateRole(ac, RoleCoder, "session_retries_exceeded")

	if got := ag.selectActiveModel(RoleCoder); got != "primary-coder" {
		t.Fatalf("selectActiveModel(RoleCoder) = %q, want primary-coder (no fallback configured)", got)
	}
	if len(emitter.escalations()) != 0 {
		t.Fatalf("expected no escalation event when no fallback is configured")
	}
}

func TestEscalateRoleOnlyEscalatesOnce(t *testing.T) {
	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		CoderModel:         "primary-coder",
		CoderFallbackModel: "fallback-coder",
	})

	ag.escalateRole(ac, RoleCoder, "session_retries_exceeded")
	ag.escalateRole(ac, RoleCoder, "session_retries_exceeded") // second call: no-op

	if len(emitter.escalations()) != 1 {
		t.Fatalf("expected exactly 1 escalation event across two calls, got %d", len(emitter.escalations()))
	}
	if got := ag.selectActiveModel(RoleCoder); got != "fallback-coder" {
		t.Fatalf("selectActiveModel(RoleCoder) = %q, want fallback-coder", got)
	}
}

func TestMidSessionEscalationTriggersAboveThreshold(t *testing.T) {
	emitter := &recordingEmitter{}
	ag, ac := newEscalationTestAgent(t, emitter, Config{
		CoderModel:         "primary-coder",
		CoderFallbackModel: "fallback-coder",
	})

	// Mirrors the exact guard added in handlePatch: escalate only once
	// ReviewRetries exceeds midSessionRetryThreshold (3).
	ac.ReviewRetries = 3
	if ac.ReviewRetries > midSessionRetryThreshold {
		ag.escalateRole(ac, RoleCoder, "session_retries_exceeded")
	}
	if got := ag.selectActiveModel(RoleCoder); got != "primary-coder" {
		t.Fatalf("at ReviewRetries=3 (not yet over threshold): selectActiveModel(RoleCoder) = %q, want primary-coder", got)
	}

	ac.ReviewRetries = 4
	if ac.ReviewRetries > midSessionRetryThreshold {
		ag.escalateRole(ac, RoleCoder, "session_retries_exceeded")
	}
	if got := ag.selectActiveModel(RoleCoder); got != "fallback-coder" {
		t.Fatalf("at ReviewRetries=4 (over threshold): selectActiveModel(RoleCoder) = %q, want fallback-coder", got)
	}

	esc := emitter.escalations()
	if len(esc) != 1 || esc[0].Payload["reason"] != "session_retries_exceeded" {
		t.Fatalf("unexpected escalation events: %+v", esc)
	}
}

// TestRunRecordsActualEscalatedModelInOutcome drives a full Run() end to end
// (real Costguard chat + feedback POST, both fake HTTP servers) with a stats
// server reporting a low pass rate, and asserts the outcome actually posted
// to /v1/feedback records the escalated fallback model, not the configured one.
func TestRunRecordsActualEscalatedModelInOutcome(t *testing.T) {
	statsSrv, _ := newStatsServer(0.10) // well below threshold
	defer statsSrv.Close()

	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := costguard.ChatResponse{
			ID: "test",
			Choices: []costguard.Choice{
				{Message: costguard.Message{Role: "assistant", Content: "FORGE_DONE: did the thing"}},
			},
			Usage: costguard.Usage{TotalTokens: 10},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer chatSrv.Close()

	received := make(chan map[string]any, 1)
	feedbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/feedback/stats" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"reviewer_pass_rate": 0.10}) //nolint:errcheck
			return
		}
		var outcome map[string]any
		json.NewDecoder(r.Body).Decode(&outcome) //nolint:errcheck
		received <- outcome
	}))
	defer feedbackSrv.Close()

	cfg := &config.Config{CostguardURL: chatSrv.URL, Timeout: 5 * time.Second}
	client := costguard.New(cfg)
	registry := NewRegistry(t.TempDir(), noopEmitter{}, "sess1", nil, nil, nil, nil)
	agentCfg := Config{
		PlannerModel:         "primary-planner",
		CoderModel:           "primary-coder",
		FeedbackEnabled:      true,
		FeedbackBaseURL:      feedbackSrv.URL,
		PlannerFallbackModel: "fallback-planner",
	}
	ag := New(agentCfg, client, registry, noopEmitter{}, confirm.AutoConfirmer{}, nil, nil, nil, nil)

	task := &compiler.Task{
		Category:        compiler.CategoryBugfix,
		Scope:           compiler.ScopeFileSpecific,
		ExecutionPolicy: compiler.PolicyAutonomous,
		RawInput:        "test task",
	}
	ac := NewAgentContext("sess1", task, t.TempDir(), nil, patch.NewPatchHistory(), nil)

	if err := ag.Run(context.Background(), ac); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case outcome := <-received:
		if outcome["planner_model"] != "primary-planner" {
			t.Errorf("planner_model = %v, want primary-planner (configured)", outcome["planner_model"])
		}
		if outcome["actual_planner_model"] != "fallback-planner" {
			t.Errorf("actual_planner_model = %v, want fallback-planner (escalated)", outcome["actual_planner_model"])
		}
		if outcome["actual_coder_model"] != "primary-coder" {
			t.Errorf("actual_coder_model = %v, want primary-coder (no coder fallback configured)", outcome["actual_coder_model"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feedback POST")
	}
}
