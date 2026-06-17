package embeddings

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const indexDir  = ".forge/index"
const indexFile = "embeddings.json"

// Load reads the existing index from root/.forge/index/embeddings.json.
// Returns (nil, nil) if no index file exists yet — absence is not an error.
func Load(root string) (*Index, error) {
	path := filepath.Join(root, indexDir, indexFile)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("embeddings: load index: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("embeddings: decode index: %w", err)
	}
	return &idx, nil
}

// Save writes idx to root/.forge/index/embeddings.json, creating directories as needed.
// TODO: shard index by file for very large repos instead of one monolithic JSON file.
func Save(root string, idx *Index) error {
	dir := filepath.Join(root, indexDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("embeddings: create index dir: %w", err)
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("embeddings: marshal index: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFile), data, 0644); err != nil {
		return fmt.Errorf("embeddings: write index: %w", err)
	}
	return nil
}

// Build walks the repo and builds (or incrementally updates) the embedding index.
// Files whose full-content hash is unchanged since the last index are skipped.
// progressFn is called after each file is processed; pass nil to disable.
//
// TODO: run Build as a background goroutine with a spinner rather than blocking startup.
func Build(
	ctx context.Context,
	root string,
	client *EmbedClient,
	existing *Index,
	progressFn func(current, total int, path string),
) (*Index, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("embeddings: abs root: %w", err)
	}

	ignorePatterns := loadIgnorePatterns(absRoot)

	// Collect all indexable files first so we can report accurate progress totals.
	var filePaths []string
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(absRoot, path)
		if rel == "." {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if matchesIgnorePatterns(rel, d.IsDir(), ignorePatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if ShouldIndex(rel) {
			filePaths = append(filePaths, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("embeddings: walk: %w", err)
	}

	// Build per-path lookups from the existing index for incremental updates.
	var oldEntries map[string][]IndexEntry
	var oldHashes map[string]string
	if existing != nil {
		oldHashes = existing.FileHashes
		oldEntries = make(map[string][]IndexEntry, len(oldHashes))
		for _, e := range existing.Entries {
			oldEntries[e.Chunk.Path] = append(oldEntries[e.Chunk.Path], e)
		}
	}

	newIndex := &Index{
		Model:      client.model,
		BuiltAt:    time.Now(),
		FileHashes: make(map[string]string, len(filePaths)),
	}

	total := len(filePaths)
	for i, rel := range filePaths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if progressFn != nil {
			progressFn(i+1, total, rel)
		}

		absPath := filepath.Join(absRoot, rel)
		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		h := sha256.Sum256(content)
		fileHash := fmt.Sprintf("%x", h)
		newIndex.FileHashes[rel] = fileHash

		// Incremental: reuse existing entries when hash is unchanged.
		// TODO: use chunk-level hashes to re-embed only changed chunks within a
		// file, rather than the whole file, for large files with small edits.
		if oldHashes != nil && oldHashes[rel] == fileHash {
			if entries, ok := oldEntries[rel]; ok {
				newIndex.Entries = append(newIndex.Entries, entries...)
				continue
			}
		}

		chunks := ChunkFile(rel, string(content))
		for _, chunk := range chunks {
			vec, err := client.Embed(ctx, chunk.Text)
			if err != nil {
				continue // skip chunk on embed error; continue with remaining chunks
			}
			newIndex.Entries = append(newIndex.Entries, IndexEntry{
				Chunk:  chunk,
				Vector: vec,
			})
		}
	}

	return newIndex, nil
}

// loadIgnorePatterns reads .gitignore from root. Inlined here to avoid
// importing internal/tools (which imports this package — would create a cycle).
func loadIgnorePatterns(root string) []string {
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// matchesIgnorePatterns mirrors the logic in internal/tools/list_files.go.
func matchesIgnorePatterns(rel string, isDir bool, patterns []string) bool {
	base := filepath.Base(rel)
	for _, p := range patterns {
		if strings.HasSuffix(p, "/") {
			dirPat := strings.TrimSuffix(p, "/")
			if isDir && (base == dirPat || rel == dirPat) {
				return true
			}
			if strings.HasPrefix(rel, dirPat+string(filepath.Separator)) {
				return true
			}
			continue
		}
		if matched, _ := filepath.Match(p, base); matched {
			return true
		}
		if rel == p {
			return true
		}
	}
	return false
}
