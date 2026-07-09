package feedback

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPostOutcomeDisabledIsNoop(t *testing.T) {
	called := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called <- struct{}{}
	}))
	defer srv.Close()

	PostOutcome(false, srv.URL, "", TaskOutcome{SessionID: "s1"})

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

	PostOutcome(true, srv.URL, "test-key", TaskOutcome{SessionID: "s2", Status: "completed"})

	select {
	case outcome := <-received:
		if outcome.SessionID != "s2" || outcome.Status != "completed" {
			t.Fatalf("unexpected outcome received: %+v", outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for PostOutcome to POST")
	}
}
