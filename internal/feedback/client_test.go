package feedback

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostOutcomeDisabledIsNoop(t *testing.T) {
	called := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
	}))
	defer srv.Close()

	PostOutcome(false, srv.URL, "", false, TaskOutcome{SessionID: "s1"})

	select {
	case <-called:
		t.Fatal("PostOutcome made an HTTP call while disabled")
	case <-time.After(200 * time.Millisecond):
		// expected: no call
	}
}

func TestPostOutcomeEnabledPosts(t *testing.T) {
	received := make(chan TaskOutcome, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/feedback" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/incorrect Authorization header: %q", r.Header.Get("Authorization"))
		}
		var outcome TaskOutcome
		if err := json.NewDecoder(r.Body).Decode(&outcome); err != nil {
			t.Errorf("decode body: %v", err)
		}
		received <- outcome
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	PostOutcome(true, srv.URL, "test-key", false, TaskOutcome{SessionID: "s2", Status: "completed"})

	select {
	case outcome := <-received:
		if outcome.SessionID != "s2" || outcome.Status != "completed" {
			t.Fatalf("unexpected outcome received: %+v", outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for PostOutcome to POST")
	}
}

// TestPostOutcomeDebugLogsSuccess proves the exact question a user asked in
// practice — "did the feedback POST actually work?" — is answerable from
// stderr alone, without needing visibility into the Costguard server: with
// debug=true, a successful POST prints a one-line confirmation.
func TestPostOutcomeDebugLogsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	PostOutcome(true, srv.URL, "", true, TaskOutcome{SessionID: "s5", Status: "completed", TotalTokensUsed: 123})
	Wait(5 * time.Second)

	os.Stderr = origStderr
	w.Close()
	out, _ := io.ReadAll(r)

	if !strings.Contains(string(out), "[feedback] posted outcome for session s5") {
		t.Fatalf("expected debug success line, got: %q", string(out))
	}
	if !strings.Contains(string(out), "tokens=123") {
		t.Fatalf("expected token count in debug line, got: %q", string(out))
	}
}

// TestWaitBlocksUntilPostCompletes reproduces the os.Exit() race: without
// Wait(), a process that exits right after calling PostOutcome can kill the
// goroutine before its request ever reaches the server. This asserts Wait()
// only returns once the server has actually received the request.
func TestWaitBlocksUntilPostCompletes(t *testing.T) {
	serverHit := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond) // simulate network/server latency
		close(serverHit)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	PostOutcome(true, srv.URL, "", false, TaskOutcome{SessionID: "s3"})
	Wait(5 * time.Second)

	select {
	case <-serverHit:
		// expected: the server was hit before Wait() returned
	default:
		t.Fatal("Wait() returned before the in-flight POST reached the server")
	}
}

// TestWaitIsInstantWhenNothingInFlight ensures Wait() never adds latency to
// the common case (FeedbackEnabled=false, or simply no recent PostOutcome
// calls) — callers on every exit path can call it unconditionally.
func TestWaitIsInstantWhenNothingInFlight(t *testing.T) {
	start := time.Now()
	Wait(5 * time.Second)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Wait() took %v with nothing in flight, want near-instant", elapsed)
	}
}

// TestWaitRespectsTimeout ensures a hung server can't block process exit
// forever — Wait() must give up after the timeout even if the POST is still
// in flight.
func TestWaitRespectsTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never respond within the test
	}))
	defer func() {
		close(block)
		srv.Close()
	}()

	PostOutcome(true, srv.URL, "", false, TaskOutcome{SessionID: "s4"})

	start := time.Now()
	Wait(300 * time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Wait() took %v, want to give up around its 300ms timeout", elapsed)
	}
}
