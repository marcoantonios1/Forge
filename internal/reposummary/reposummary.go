package reposummary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const cacheDir  = ".forge/memory"
const cacheFile = "repo_summary.json"

func cachePath(root string) string {
	return filepath.Join(root, cacheDir, cacheFile)
}

// HashFileList computes a stable cache key from a list of file paths.
// Callers are responsible for sorting files before calling this — list_files
// already returns a sorted result.
//
// TODO: consider including a forge.md content hash in the cache key if
// forge.md edits should invalidate the repo summary cache.
func HashFileList(files []string) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func loadCache(root string) (*CacheEntry, error) {
	data, err := os.ReadFile(cachePath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reposummary: reading cache: %w", err)
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("reposummary: parsing cache: %w", err)
	}
	return &entry, nil
}

func saveCache(root string, entry *CacheEntry) error {
	// TODO: a future `forge memory clear --all` could prune repo_summary.json
	// alongside memory.json, mirroring the existing `forge memory clear` command.
	path := cachePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("reposummary: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("reposummary: marshal: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// keyFileNames lists key project files checked for existence, in priority order.
// Intentionally duplicated from internal/forgeinit — these packages serve
// different purposes and should not be coupled.
var keyFileNames = []string{
	"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "setup.py",
	"Makefile", "README.md", "build.gradle", "build.gradle.kts", "Dockerfile",
}

func findKeyFiles(root string) []string {
	var found []string
	for _, name := range keyFileNames {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

// buildDirTree renders a max-depth directory tree from a flat file list
// (relative paths as returned by list_files). Derives structure purely from
// path strings — no second filesystem walk.
func buildDirTree(files []string, maxDepth int) string {
	dirs := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(f, string(filepath.Separator))
		for i := 1; i <= len(parts) && i <= maxDepth; i++ {
			dirs[strings.Join(parts[:i], "/")] = true
		}
	}
	var sorted []string
	for d := range dirs {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	var sb strings.Builder
	for _, d := range sorted {
		depth := strings.Count(d, "/")
		sb.WriteString(strings.Repeat("  ", depth))
		sb.WriteString(filepath.Base(d))
		sb.WriteString("/\n")
	}
	return sb.String()
}

// extToLang maps file extensions to language names.
// Intentionally duplicated from internal/forgeinit — see note on keyFileNames.
var extToLang = map[string]string{
	".go": "Go", ".ts": "TypeScript", ".tsx": "TypeScript",
	".js": "JavaScript", ".jsx": "JavaScript", ".py": "Python",
	".rs": "Rust", ".java": "Java", ".rb": "Ruby", ".cs": "C#",
	".cpp": "C++", ".cc": "C++", ".cxx": "C++", ".c": "C",
}

func detectLanguagesFromFiles(files []string) []string {
	seen := map[string]bool{}
	for _, f := range files {
		if lang, ok := extToLang[filepath.Ext(f)]; ok {
			seen[lang] = true
		}
	}
	var out []string
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// ChatFunc abstracts the Costguard call so this package doesn't import
// internal/costguard directly — keeps reposummary decoupled and testable.
type ChatFunc func(ctx context.Context, model, systemPrompt, userPrompt string) (string, error)

// Generate returns a repo summary, using the cache when the file-list hash
// is unchanged, otherwise generating a new one via chat and caching it.
// forgeMDBlock may be empty.
//
// Returns (summary, fromCache, error). On any chat/model error the structural
// facts are returned as a best-effort summary — the error is non-fatal and
// the caller may log it but must not treat it as task-failing.
//
// TODO: enforce the 200-word cap programmatically (e.g. truncate narrative
// to N words) rather than relying solely on prompt instruction compliance.
func Generate(
	ctx context.Context,
	root string,
	files []string,
	forgeMDBlock string,
	chat ChatFunc,
	model string,
) (summary string, fromCache bool, err error) {
	cacheKey := HashFileList(files)

	if cached, cerr := loadCache(root); cerr == nil && cached != nil && cached.CacheKey == cacheKey {
		return cached.Summary, true, nil
	}

	dirTree := buildDirTree(files, 2)
	languages := detectLanguagesFromFiles(files)
	keyFiles := findKeyFiles(root)

	var sb strings.Builder
	sb.WriteString("Directory structure (max 2 levels):\n")
	sb.WriteString(dirTree)
	sb.WriteString("\nDetected languages: ")
	sb.WriteString(strings.Join(languages, ", "))
	sb.WriteString("\nKey files: ")
	sb.WriteString(strings.Join(keyFiles, ", "))
	if forgeMDBlock != "" {
		sb.WriteString("\n\nforge.md conventions:\n")
		sb.WriteString(forgeMDBlock)
	}

	userPrompt := "Summarize this repo structure in 200 words or less:\n\n" + sb.String()

	narrative, chatErr := chat(ctx, model,
		"You write concise, factual repo structure summaries for an autonomous coding agent's context window. No preamble, no markdown headers — plain prose, 200 words maximum.",
		userPrompt)
	if chatErr != nil {
		narrative = sb.String()
	}

	entry := &CacheEntry{CacheKey: cacheKey, Summary: narrative, GeneratedAt: time.Now()}
	if serr := saveCache(root, entry); serr != nil {
		if chatErr == nil {
			chatErr = serr
		}
	}

	return narrative, false, chatErr
}
