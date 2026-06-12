package patch

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileError struct {
	Path    string
	Message string
}

type ValidationResult struct {
	Valid  bool
	Errors []FileError
}

// Validate runs pre-flight checks on a PatchSet against the repo at root.
// Does NOT modify any files. Collects all errors in one pass.
func Validate(root string, ps *PatchSet) ValidationResult {
	var errs []FileError

	add := func(path, msg string) {
		errs = append(errs, FileError{Path: path, Message: msg})
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ValidationResult{Errors: []FileError{{Path: root, Message: "invalid root: " + err.Error()}}}
	}

	for _, p := range ps.Patches {
		if p.Path == "" {
			add(p.Path, "empty path")
			continue
		}

		// 1. Traversal check.
		abs := filepath.Join(absRoot, p.Path)
		if !strings.HasPrefix(abs, absRoot+string(filepath.Separator)) && abs != absRoot {
			add(p.Path, "path traversal not allowed")
			continue
		}

		// 2. File must exist (or be a new-file patch).
		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				if p.IsNew {
					for hi, h := range p.Hunks {
						if h.OldStart != 0 || h.OldLines != 0 {
							add(p.Path, fmt.Sprintf(
								"hunk %d: new file patch must have OldStart=0 OldLines=0, got %d/%d",
								hi+1, h.OldStart, h.OldLines))
						}
						for _, l := range h.Lines {
							if len(l) > 0 && l[0] == '-' {
								add(p.Path, fmt.Sprintf(
									"hunk %d: new file patch must not contain '-' lines", hi+1))
								break
							}
						}
					}
					continue
				}
				add(p.Path, "file not found: check the path is correct relative to the repo root")
			} else {
				add(p.Path, "stat error: "+err.Error())
			}
			continue
		}
		if info.IsDir() {
			add(p.Path, "path is a directory")
			continue
		}

		data, err := os.ReadFile(abs)
		if err != nil {
			add(p.Path, "read error: "+err.Error())
			continue
		}

		// 3. Binary check — first 8KB for null bytes.
		check := data
		if len(check) > 8192 {
			check = check[:8192]
		}
		if bytes.IndexByte(check, 0) != -1 {
			add(p.Path, "binary file not supported")
			continue
		}

		fileLines := strings.Split(string(data), "\n")

		// 4. Validate each hunk's context against actual file content.
		for hi, h := range p.Hunks {
			if err := validateHunk(fileLines, h, p.Path, hi+1); err != "" {
				add(p.Path, err)
			}
		}
	}

	return ValidationResult{Valid: len(errs) == 0, Errors: errs}
}

func validateHunk(fileLines []string, h Hunk, path string, hunkIdx int) string {
	fileIdx := h.OldStart - 1 // convert to 0-indexed
	for _, line := range h.Lines {
		if len(line) == 0 {
			continue
		}
		prefix := line[0]
		content := line[1:]

		switch prefix {
		case ' ', '-':
			if fileIdx >= len(fileLines) {
				return fmt.Sprintf("hunk %d: line %d out of range (file has %d lines)", hunkIdx, fileIdx+1, len(fileLines))
			}
			if fileLines[fileIdx] != content {
				return fmt.Sprintf("hunk %d: context mismatch at line %d: expected %q, got %q",
					hunkIdx, fileIdx+1, content, fileLines[fileIdx])
			}
			fileIdx++
		case '+':
			// Added lines don't consume original file lines.
		}
	}
	return ""
}
