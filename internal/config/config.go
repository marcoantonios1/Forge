package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type ModelLimits struct {
	CompilerMaxTokens   int
	PlannerMaxTokens    int
	CoderMaxTokens      int
	ToolCallerMaxTokens int
	CompactorMaxTokens  int
	ReviewerMaxTokens      int
	ReviewerContextTokens  int
	EmbeddingMaxTokens     int

	// ContextTokens are the compaction thresholds — if the estimated input
	// token count exceeds this, older history is summarised before the call.
	// Each falls back to the corresponding MaxTokens value if unset (0).
	// CompilerContextTokens is the base fallback: when a role-specific
	// ContextTokens and its MaxTokens are both unset, this is used instead,
	// mirroring how CompilerMaxTokens is the base model fallback.
	CompilerContextTokens   int
	PlannerContextTokens    int
	CoderContextTokens      int
	ToolCallerContextTokens int
	CompactorContextTokens  int
}

type Config struct {
	CostguardURL      string
	CostguardAgent    string
	CostguardProvider string
	CostguardTeam     string
	CostguardProject  string
	Timeout           time.Duration
	MaxRetries        int
	Debug             bool
	CompilerModel     string
	PlannerModel      string
	CoderModel        string
	ToolCallerModel   string // empty = tool-caller disabled, planner emits TOOL:/ARGS: directly
	CompactorModel    string
	ReviewerModel     string
	EmbeddingModel    string
	Limits            ModelLimits
	FeedbackEnabled   bool   // FORGE_FEEDBACK_ENABLED=true — off by default
	FeedbackAPIKey    string // FEEDBACK_API_KEY — sent as "Authorization: Bearer <key>" on /v1/feedback POSTs and /v1/feedback/stats GETs; empty = no auth header

	// Fallback models — escalated to when a role's reviewer pass rate falls
	// below its threshold. Empty = no escalation for that role.
	PlannerFallbackModel  string // FORGE_PLANNER_FALLBACK_MODEL
	CoderFallbackModel    string // FORGE_CODER_FALLBACK_MODEL
	ReviewerFallbackModel string // FORGE_REVIEWER_FALLBACK_MODEL

	// FallbackThreshold — reviewer pass rate (0.0-1.0) below which pre-emptive
	// escalation triggers at session start. Applied to all roles uniformly.
	// Default: 0.60 (i.e. 60% pass rate threshold).
	FallbackThreshold float64 // FORGE_FALLBACK_THRESHOLD
}

// loadDotEnv reads .env from the current directory and sets any variables
// not already present in the environment. Actual env vars always win.
func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip inline comments (e.g. "VAL=foo  # comment").
		if idx := strings.Index(line, " #"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		// Strip optional "export " prefix so both bare and shell-style .env files work.
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func Load() (*Config, error) {
	loadDotEnv()
	cfg := &Config{
		CostguardURL:   "http://localhost:8080",
		CostguardAgent: "forge",
		Timeout:         60 * time.Second,
		MaxRetries:      3,
		CompilerModel:   "claude-sonnet-4-6",
		PlannerModel:    "claude-sonnet-4-6",
		CoderModel:      "claude-sonnet-4-6",
		ToolCallerModel: "", // unset by default — backwards-compatible direct-call path
		CompactorModel:  "claude-sonnet-4-6",
		EmbeddingModel:  "",
		Limits: ModelLimits{
			CompilerMaxTokens:   8000,
			PlannerMaxTokens:    16000,
			CoderMaxTokens:      16000,
			ToolCallerMaxTokens: 4000,
			CompactorMaxTokens:    8000,
			ReviewerMaxTokens:     8000,
			ReviewerContextTokens: 32000,
			EmbeddingMaxTokens:    8000,
		},
	}

	if v := os.Getenv("COSTGUARD_URL"); v != "" {
		cfg.CostguardURL = v
	}
if v := os.Getenv("COSTGUARD_AGENT"); v != "" {
		cfg.CostguardAgent = v
	}
	if v := os.Getenv("COSTGUARD_PROVIDER"); v != "" {
		cfg.CostguardProvider = v
	}
	if v := os.Getenv("COSTGUARD_TEAM"); v != "" {
		cfg.CostguardTeam = v
	}
	if v := os.Getenv("COSTGUARD_PROJECT"); v != "" {
		cfg.CostguardProject = v
	}
	// COMPILER_MODEL kept for backwards compatibility with existing .env files.
	if v := os.Getenv("COMPILER_MODEL"); v != "" {
		cfg.CompilerModel = v
	}
	if v := os.Getenv("FORGE_COMPILER_MODEL"); v != "" {
		cfg.CompilerModel = v
	}
	if v := os.Getenv("FORGE_PLANNER_MODEL"); v != "" {
		cfg.PlannerModel = v
	}
	if v := os.Getenv("FORGE_CODER_MODEL"); v != "" {
		cfg.CoderModel = v
	}
	if v := os.Getenv("FORGE_TOOL_CALLER_MODEL"); v != "" {
		cfg.ToolCallerModel = v
	}
	if v := os.Getenv("FORGE_COMPACTOR_MODEL"); v != "" {
		cfg.CompactorModel = v
	}
	if v := os.Getenv("FORGE_EMBEDDING_MODEL"); v != "" {
		cfg.EmbeddingModel = v
	}
	if v := os.Getenv("FORGE_COMPILER_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.CompilerMaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_PLANNER_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.PlannerMaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_CODER_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.CoderMaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_TOOL_CALLER_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.ToolCallerMaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_COMPACTOR_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.CompactorMaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_REVIEWER_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.ReviewerMaxTokens = n
		}
	}
	if v := os.Getenv("FORGE_REVIEWER_CONTEXT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.ReviewerContextTokens = n
		}
	}
	if v := os.Getenv("FORGE_EMBEDDING_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Limits.EmbeddingMaxTokens = n
		}
	}
	// Context token limits (compaction thresholds). Each falls back to the
	// corresponding MAX_TOKENS value when the CONTEXT_TOKENS var is unset,
	// preserving backwards compatibility with existing .env files.
	parseContextTokens := func(envVar string, fallback int) int {
		if v := os.Getenv(envVar); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
		return fallback
	}
	// CompilerContextTokens is resolved first so it can serve as the base
	// fallback for all other roles, mirroring how CompilerMaxTokens is used.
	cfg.Limits.CompilerContextTokens   = parseContextTokens("FORGE_COMPILER_CONTEXT_TOKENS", cfg.Limits.CompilerMaxTokens)
	cfg.Limits.PlannerContextTokens    = parseContextTokens("FORGE_PLANNER_CONTEXT_TOKENS", cfg.Limits.PlannerMaxTokens)
	cfg.Limits.CoderContextTokens      = parseContextTokens("FORGE_CODER_CONTEXT_TOKENS", cfg.Limits.CoderMaxTokens)
	cfg.Limits.ToolCallerContextTokens = parseContextTokens("FORGE_TOOL_CALLER_CONTEXT_TOKENS", cfg.Limits.ToolCallerMaxTokens)
	cfg.Limits.CompactorContextTokens  = parseContextTokens("FORGE_COMPACTOR_CONTEXT_TOKENS", cfg.Limits.CompactorMaxTokens)
	// Fallback: PlannerModel, CoderModel, CompactorModel fall back to CompilerModel
	// if left empty. ToolCallerModel and EmbeddingModel do NOT fall back — empty
	// means "feature disabled", which is the intended default.
	if cfg.PlannerModel == "" {
		cfg.PlannerModel = cfg.CompilerModel
	}
	if cfg.CoderModel == "" {
		cfg.CoderModel = cfg.CompilerModel
	}
	if cfg.CompactorModel == "" {
		cfg.CompactorModel = cfg.CompilerModel
	}
	// ReviewerModel: an EXPLICITLY EMPTY FORGE_REVIEWER_MODEL="" disables review
	// (opt-out for speed). An unset env var falls back to PlannerModel so the
	// reviewer inherits the strongest model by default.
	if v, present := os.LookupEnv("FORGE_REVIEWER_MODEL"); present {
		cfg.ReviewerModel = v // may be "" — that is the explicit opt-out signal
	} else {
		cfg.ReviewerModel = cfg.PlannerModel // resolved above, so fallback is correct
	}
	if v := os.Getenv("COSTGUARD_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("config: invalid COSTGUARD_TIMEOUT %q: %w", v, err)
		}
		cfg.Timeout = d
	}
	if v := os.Getenv("COSTGUARD_MAX_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("config: invalid COSTGUARD_MAX_RETRIES %q", v)
		}
		cfg.MaxRetries = n
	}
	if v := os.Getenv("FORGE_DEBUG"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("config: invalid FORGE_DEBUG %q: %w", v, err)
		}
		cfg.Debug = b
	}
	// No default — FeedbackEnabled is false unless explicitly set.
	// TODO: when FeedbackEnabled defaults to true in a future release, also
	// parse "false"/"0" for explicit opt-out.
	if v := os.Getenv("FORGE_FEEDBACK_ENABLED"); strings.EqualFold(v, "true") || v == "1" {
		cfg.FeedbackEnabled = true
	}
	if v := os.Getenv("FEEDBACK_API_KEY"); v != "" {
		cfg.FeedbackAPIKey = v
	}
	if v := os.Getenv("FORGE_PLANNER_FALLBACK_MODEL"); v != "" {
		cfg.PlannerFallbackModel = v
	}
	if v := os.Getenv("FORGE_CODER_FALLBACK_MODEL"); v != "" {
		cfg.CoderFallbackModel = v
	}
	if v := os.Getenv("FORGE_REVIEWER_FALLBACK_MODEL"); v != "" {
		cfg.ReviewerFallbackModel = v
	}
	cfg.FallbackThreshold = 0.60 // default
	if v := os.Getenv("FORGE_FALLBACK_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			cfg.FallbackThreshold = f
		}
	}

	return cfg, nil
}
