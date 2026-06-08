package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	CostguardURL    string
	CostguardAPIKey string
	Mode            string
	Timeout         time.Duration
	MaxRetries      int
	Debug           bool
}

func Load() (*Config, error) {
	cfg := &Config{
		CostguardURL: "http://localhost:8080",
		Mode:         "balanced",
		Timeout:      60 * time.Second,
		MaxRetries:   3,
	}

	if v := os.Getenv("COSTGUARD_URL"); v != "" {
		cfg.CostguardURL = v
	}
	if v := os.Getenv("COSTGUARD_API_KEY"); v != "" {
		cfg.CostguardAPIKey = v
	}
	if v := os.Getenv("COSTGUARD_MODE"); v != "" {
		cfg.Mode = v
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
