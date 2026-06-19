package reposummary

import "time"

// CacheEntry holds a generated repo summary keyed by a hash of the file list.
type CacheEntry struct {
	CacheKey    string    `json:"cache_key"`   // sha256 of sorted file list
	Summary     string    `json:"summary"`
	GeneratedAt time.Time `json:"generated_at"`
}
