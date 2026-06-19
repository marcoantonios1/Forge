package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const sessionsDir      = ".forge/sessions"
const maxHistoryEntries = 20

func sessionPath(root string) string {
	return filepath.Join(root, sessionsDir, "last.json")
}

// Save writes sess to <root>/.forge/sessions/last.json, capping History to
// the most recent maxHistoryEntries entries. Always overwrites — only the
// last session per repo is kept.
func Save(root string, sess *SavedSession) error {
	if len(sess.History) > maxHistoryEntries {
		sess.History = sess.History[len(sess.History)-maxHistoryEntries:]
	}
	path := sessionPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("session: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("session: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("session: write: %w", err)
	}
	// Best-effort registry update — session save must not fail because of this.
	_ = RegisterRepo(root)
	return nil
}

// Load reads the last saved session for root. Returns (nil, nil) if none exists.
func Load(root string) (*SavedSession, error) {
	data, err := os.ReadFile(sessionPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("session: reading: %w", err)
	}
	var sess SavedSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("session: parsing: %w", err)
	}
	return &sess, nil
}

// ListedSession is a summary row for `forge sessions list`.
type ListedSession struct {
	Repo      string `json:"repo"`
	SessionID string `json:"session_id"`
	RawInput  string `json:"raw_input"`
	Files     int    `json:"files"`
	Timestamp string `json:"timestamp"`
}

// List checks each known repo root for a saved session and returns summary
// rows, most recent first.
func List(roots []string) []ListedSession {
	var out []ListedSession
	for _, root := range roots {
		sess, err := Load(root)
		if err != nil || sess == nil {
			continue
		}
		fileSet := map[string]bool{}
		for _, rec := range sess.Patches {
			for path := range rec.Originals {
				fileSet[path] = true
			}
		}
		out = append(out, ListedSession{
			Repo:      root,
			SessionID: sess.SessionID,
			RawInput:  sess.RawInput,
			Files:     len(fileSet),
			Timestamp: sess.Timestamp.Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp > out[j].Timestamp
	})
	return out
}

// RegisterRepo appends root to the global known-repos list at
// ~/.forge/known_repos.json (deduplicated). This is the only piece of Forge
// state stored outside a project's own .forge/ directory — it exists solely
// to make `forge sessions list` work across repos without a central server.
//
// TODO: ~/.forge/known_repos.json can grow unbounded as repos are created
// and deleted. A future `forge sessions list --prune` could drop entries
// whose .forge/sessions/last.json no longer exists on disk.
func RegisterRepo(root string) error {
	path, err := globalRegistryPath()
	if err != nil {
		return err
	}
	var repos []string
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &repos) //nolint:errcheck
	}
	for _, r := range repos {
		if r == root {
			return nil // already registered
		}
	}
	repos = append(repos, root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(repos, "", "  ")
	return os.WriteFile(path, data, 0644)
}

// KnownRepos reads the global registry. Returns nil (not an error) if the
// registry doesn't exist yet.
func KnownRepos() []string {
	path, err := globalRegistryPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var repos []string
	json.Unmarshal(data, &repos) //nolint:errcheck
	return repos
}

func globalRegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge", "known_repos.json"), nil
}
