// Package forgeinit implements `forge init`: pure filesystem heuristics that
// detect a project's build/test commands and languages, with no LLM call.
package forgeinit

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Detection struct {
	BuildCmd  string   // e.g. "make build", "go build ./...", "npm run build"
	TestCmd   string   // e.g. "make test", "go test ./...", "npm test"
	Languages []string // e.g. ["Go", "TypeScript"]
	HasGit    bool     // .git directory present
	HasDocker bool     // Dockerfile present
}

var extToLang = map[string]string{
	".go":   "Go",
	".ts":   "TypeScript",
	".tsx":  "TypeScript",
	".js":   "JavaScript",
	".jsx":  "JavaScript",
	".py":   "Python",
	".rs":   "Rust",
	".java": "Java",
	".rb":   "Ruby",
	".cs":   "C#",
	".cpp":  "C++",
	".cc":   "C++",
	".cxx":  "C++",
	".c":    "C",
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Detect analyses dir and returns a Detection. All fields may be empty
// strings / nil if nothing is found. Never returns an error — missing files
// are silently skipped.
//
// TODO: additional indicators (Cargo.lock, yarn.lock, poetry.lock) would
// refine the detection.
func Detect(dir string) Detection {
	var d Detection

	hasMakefile := exists(filepath.Join(dir, "Makefile"))
	hasGoMod := exists(filepath.Join(dir, "go.mod"))
	hasPackageJSON := exists(filepath.Join(dir, "package.json"))
	hasCargoToml := exists(filepath.Join(dir, "Cargo.toml"))
	hasPyProject := exists(filepath.Join(dir, "pyproject.toml")) || exists(filepath.Join(dir, "setup.py"))
	hasGradle := exists(filepath.Join(dir, "build.gradle")) || exists(filepath.Join(dir, "build.gradle.kts"))

	switch {
	case hasMakefile && makefileHasTarget(dir, "build"):
		d.BuildCmd = "make build"
	case hasGoMod:
		d.BuildCmd = "go build ./..."
	case hasPackageJSON:
		d.BuildCmd = "npm run build"
	case hasCargoToml:
		d.BuildCmd = "cargo build"
	case hasPyProject:
		d.BuildCmd = "python -m build"
	case hasGradle:
		d.BuildCmd = "./gradlew build"
	}

	switch {
	case hasMakefile && makefileHasTarget(dir, "test"):
		d.TestCmd = "make test"
	case hasGoMod:
		d.TestCmd = "go test ./..."
	case hasPackageJSON:
		d.TestCmd = "npm test"
	case hasCargoToml:
		d.TestCmd = "cargo test"
	case hasPyProject:
		d.TestCmd = "pytest"
	case hasGradle:
		d.TestCmd = "./gradlew test"
	}

	d.Languages = detectLanguages(dir)
	d.HasGit = exists(filepath.Join(dir, ".git"))
	d.HasDocker = exists(filepath.Join(dir, "Dockerfile"))

	return d
}

// detectLanguages walks the top two directory levels (skipping hidden dirs)
// and maps file extensions found to language names.
func detectLanguages(root string) []string {
	found := make(map[string]bool)
	walkLanguages(root, 1, found)

	langs := make([]string, 0, len(found))
	for l := range found {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

func walkLanguages(dir string, depth int, found map[string]bool) {
	if depth > 2 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			walkLanguages(filepath.Join(dir, name), depth+1, found)
			continue
		}
		if lang, ok := extToLang[filepath.Ext(name)]; ok {
			found[lang] = true
		}
	}
}

// makefileHasTarget reads the Makefile line by line (Makefiles can be large)
// and returns true if any line matches `^<target>\s*:`.
func makefileHasTarget(dir, target string) bool {
	f, err := os.Open(filepath.Join(dir, "Makefile"))
	if err != nil {
		return false
	}
	defer f.Close()

	re := regexp.MustCompile(`^` + regexp.QuoteMeta(target) + `\s*:`)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			return true
		}
	}
	return false
}
