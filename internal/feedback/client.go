package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// PostOutcome ships outcome to Costguard's /v1/feedback endpoint in a
// fire-and-forget goroutine. Errors are logged to stderr but never propagate
// — feedback failures must never affect the task's success path.
//
// baseURL is cfg.CostguardURL (e.g. "http://localhost:8080").
// apiKey is cfg.CostguardAPIKey (may be empty for local Costguard).
//
// PostOutcome is a no-op if enabled is false — callers do not need to
// check the flag themselves; this keeps call sites clean.
//
// TODO: retry once (after ~2s) on transient network errors once the
// /v1/feedback endpoint is considered stable — excluded here to keep the
// implementation simple for v1.
func PostOutcome(enabled bool, baseURL, apiKey string, outcome TaskOutcome) {
	if !enabled {
		return
	}
	go func() {
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
		}
	}()
}
