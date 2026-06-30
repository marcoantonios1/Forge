package codeintel

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WalkFiles returns all non-hidden, non-gitignored files under root whose
// extension is in the allowedExts set (nil/empty = no extension filter).
// Mirrors internal/tools/list_files.go's walk exactly (hidden-dir skip,
// .gitignore prefix/glob matching) — duplicated intentionally, consistent
// with the existing internal/embeddings precedent, to avoid an import cycle
// (internal/tools will import internal/codeintel for the tool wrappers).
func WalkFiles(root string, allowedExts map[string]bool) ([]string, error) {
	ignorePatterns := loadGitignore(root)

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		rel, _ := filepath.Rel(root, path)
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

		if matchesGitignore(rel, d.IsDir(), ignorePatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if len(allowedExts) > 0 {
			ext := filepath.Ext(name)
			if !allowedExts[ext] {
				return nil
			}
		}

		files = append(files, rel)
		return nil
	})
	return files, err
}

func loadGitignore(root string) []string {
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			// TODO: negation patterns are out of scope — skip silently.
			continue
		}
		patterns = append(patterns, line)
	}
	if scanner.Err() != nil {
		return nil
	}
	return patterns
}

func matchesGitignore(rel string, isDir bool, patterns []string) bool {
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
