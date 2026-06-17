package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	CostguardURL      string
	Mode              string
	CostguardAgent    string
	CostguardProvider string
	CostguardTeam     string
	CostguardProject  string
	Timeout           time.Duration
	MaxRetries        int
	Debug             bool
	CompilerModel   string
	PlannerModel    string
	CoderModel      string
	ToolCallerModel string // empty = tool-caller disabled, planner emits TOOL:/ARGS: directly
	CompactorModel  string
	EmbeddingModel  string
	// TODO: EmbeddingModel is configured but not yet consumed — wire into the
	// semantic search / embedding pipeline ticket when implemented.
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
		Mode:           "balanced",
		CostguardAgent: "forge",
		Timeout:        60 * time.Second,
		MaxRetries:     3,
		CompilerModel:   "claude-sonnet-4-6",
		PlannerModel:    "claude-sonnet-4-6",
		CoderModel:      "claude-sonnet-4-6",
		ToolCallerModel: "", // unset by default — backwards-compatible direct-call path
		CompactorModel:  "claude-sonnet-4-6",
		EmbeddingModel:  "",
	}

	if v := os.Getenv("COSTGUARD_URL"); v != "" {
		cfg.CostguardURL = v
	}
	if v := os.Getenv("COSTGUARD_MODE"); v != "" {
		cfg.Mode = v
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

	return cfg, nil
}
