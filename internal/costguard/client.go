package costguard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/marcoantonios1/Forge/internal/config"
)

type Client struct {
	cfg    *config.Config
	http   *http.Client
	base   string
}

func New(cfg *config.Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
		base: strings.TrimRight(cfg.CostguardURL, "/"),
	}
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("costguard: marshal request: %w", err)
	}

	url := c.base + "/v1/chat/completions"

	var (
		resp    *ChatResponse
		attempt int
		base    = 200 * time.Millisecond
		maxWait = 10 * time.Second
	)

	for {
		attempt++
		start := time.Now()

		if c.cfg.Debug {
			fmt.Fprintf(os.Stderr, "[costguard] → POST /v1/chat/completions model=%s\n", req.Model)
		}

		httpResp, err := c.do(ctx, url, body)
		if err != nil {
			return nil, err
		}

		elapsed := time.Since(start)
		statusCode := httpResp.StatusCode
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()

		if statusCode >= 200 && statusCode < 300 {
			var cr ChatResponse
			if err := json.Unmarshal(respBody, &cr); err != nil {
				return nil, fmt.Errorf("costguard: decode response: %w", err)
			}
			if c.cfg.Debug {
				fmt.Fprintf(os.Stderr, "[costguard] ← %d in %dms tokens=%d\n",
					statusCode, elapsed.Milliseconds(), cr.Usage.TotalTokens)
			}
			resp = &cr
			return resp, nil
		}

		cgErr := parseError(statusCode, respBody)

		// No retry for client errors (except 429) or budget exceeded.
		if !isRetryable(statusCode) || attempt > c.cfg.MaxRetries {
			return nil, cgErr
		}

		wait := backoff(base, attempt, maxWait)
		if c.cfg.Debug {
			fmt.Fprintf(os.Stderr, "[costguard] ← %d retrying in %dms (attempt %d/%d)\n",
				statusCode, wait.Milliseconds(), attempt, c.cfg.MaxRetries)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (c *Client) do(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("costguard: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Costguard-Agent", c.cfg.CostguardAgent)
	req.Header.Set("X-Costguard-Mode", c.cfg.Mode)
	if c.cfg.CostguardProvider != "" {
		req.Header.Set("X-Costguard-Provider", c.cfg.CostguardProvider)
	}
	if c.cfg.CostguardTeam != "" {
		req.Header.Set("X-Costguard-Team", c.cfg.CostguardTeam)
	}
	if c.cfg.CostguardProject != "" {
		req.Header.Set("X-Costguard-Project", c.cfg.CostguardProject)
	}
	return c.http.Do(req)
}

func parseError(statusCode int, body []byte) *CostguardError {
	var wrapper struct {
		Error struct {
			Message  string `json:"message"`
			Type     string `json:"type"`
			Category string `json:"category"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &wrapper)

	category := wrapper.Error.Category
	if category == "" {
		category = inferCategory(statusCode)
	}

	return &CostguardError{
		StatusCode: statusCode,
		Category:   category,
		Message:    wrapper.Error.Message,
		Type:       wrapper.Error.Type,
	}
}

func inferCategory(code int) string {
	switch code {
	case 401, 403:
		return "auth"
	case 402:
		return "budget_exceeded"
	case 429:
		return "rate_limit"
	case 502, 503:
		return "provider_unavailable"
	default:
		return "unknown"
	}
}

func isRetryable(code int) bool {
	return code == 429 || code == 502 || code == 503
}

// backoff returns exponential backoff with ±10% jitter.
func backoff(base time.Duration, attempt int, max time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > max {
			d = max
			break
		}
	}
	// ±10% jitter
	jitter := float64(d) * 0.1
	d += time.Duration((rand.Float64()*2-1)*jitter)
	if d < 0 {
		d = 0
	}
	return d
}
