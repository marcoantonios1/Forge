package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// inFlight tracks PostOutcome goroutines that have not yet completed. A
// process about to exit (e.g. --print/headless mode, which os.Exit()s right
// after its top-level function returns) must call Wait() first — otherwise
// os.Exit() kills the fire-and-forget goroutine before its HTTP request ever
// leaves the machine, silently dropping the outcome.
var inFlight sync.WaitGroup

// PostOutcome ships outcome to Costguard's /v1/feedback endpoint in a
// fire-and-forget goroutine. Errors are logged to stderr but never propagate
// — feedback failures must never affect the task's success path.
//
// baseURL is cfg.CostguardURL (e.g. "http://localhost:8080").
// apiKey is cfg.CostguardAPIKey (may be empty for local Costguard).
// debug mirrors the --debug flag: when true, a successful POST also prints a
// one-line confirmation to stderr (mirroring [costguard]'s trace style) so
// FORGE_FEEDBACK_ENABLED can be verified without needing visibility into the
// Costguard server. Failures are always logged regardless of debug.
//
// PostOutcome is a no-op if enabled is false — callers do not need to
// check the flag themselves; this keeps call sites clean.
//
// TODO: retry once (after ~2s) on transient network errors once the
// /v1/feedback endpoint is considered stable — excluded here to keep the
// implementation simple for v1.
func PostOutcome(enabled bool, baseURL, apiKey string, debug bool, outcome TaskOutcome) {
	if !enabled {
		return
	}
	inFlight.Add(1)
	go func() {
		defer inFlight.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		body, err := json.Marshal(outcome)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[feedback] marshal error: %v\n", err)
			return
		}

		url := strings.TrimRight(baseURL, "/") + "/v1/feedback"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "[feedback] request error: %v\n", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		req.Header.Set("X-Costguard-Agent", "forge")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[feedback] post error: %v\n", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			fmt.Fprintf(os.Stderr, "[feedback] server returned %d for /v1/feedback\n", resp.StatusCode)
			return
		}
		if debug {
			fmt.Fprintf(os.Stderr, "[feedback] posted outcome for session %s: status=%s tokens=%d (http %d)\n",
				outcome.SessionID, outcome.Status, outcome.TotalTokensUsed, resp.StatusCode)
		}
	}()
}

// Wait blocks until every PostOutcome call started so far has completed, or
// until timeout elapses, whichever comes first. It is always safe to call —
// if nothing is in flight (including when FeedbackEnabled is false, since
// PostOutcome never spawns a goroutine in that case), it returns immediately.
//
// Callers that are about to terminate the process (os.Exit, or falling off
// the end of main) should call this first so a fire-and-forget feedback POST
// isn't silently killed mid-flight. Long-running processes (the REPL, between
// tasks) do not need to call this — their goroutines have the rest of the
// process lifetime to complete on their own.
func Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
