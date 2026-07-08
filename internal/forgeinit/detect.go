// Package forgeinit implements `forge init`: pure filesystem heuristics that
// detect a project's build/test commands and languages, with no LLM call.
package forgeinit

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Detection struct {
	BuildCmd            string   // e.g. "make build", "go build ./...", "npm run build"
	TestCmd             string   // e.g. "make test", "go test ./...", "npm test"
	Languages           []string // e.g. ["Go", "TypeScript"]
	HasGit              bool     // .git directory present
	HasDocker           bool     // Dockerfile present
	SuggestedMCPServers []MCPSuggestion
}

// MCPSuggestion is a recommended MCP server config block for forge.md.
type MCPSuggestion struct {
	Name    string // e.g. "playwright"
	Comment string // why it was suggested, shown as a comment above the block
	Block   string // the full [[mcp.servers]] TOML block, ready to paste
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
	d.SuggestedMCPServers = detectMCPServers(dir, hasPackageJSON, hasGoMod)

	return d
}

// detectMCPServers checks repo contents for indicators that suggest
// specific MCP servers would be useful for this project.
//
// TODO: additional MCP server detections could be added (Slack, Jira,
// Linear, databases) — the pattern is the same: check for a dep name in
// package.json or a tool on PATH, append a MCPSuggestion — just extend the
// list below.
func detectMCPServers(dir string, hasPackageJSON, hasGoMod bool) []MCPSuggestion {
	var suggestions []MCPSuggestion

	// Playwright → browser automation MCP server
	if hasPackageJSON && packageJSONHasDep(dir, "playwright", "@playwright/test") {
		suggestions = append(suggestions, MCPSuggestion{
			Name:    "playwright",
			Comment: "Playwright detected in package.json — browser automation via MCP",
			Block: `[[mcp.servers]]
name = "playwright"
transport = "stdio"
command = "npx"
args = ["@playwright/mcp@latest"]`,
		})
	}

	// GitHub CLI available → GitHub MCP server
	if commandExists("gh") {
		suggestions = append(suggestions, MCPSuggestion{
			Name:    "github",
			Comment: "GitHub CLI (gh) detected — issues, PRs, and releases via MCP",
			Block: `[[mcp.servers]]
name = "github"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env.GITHUB_PERSONAL_ACCESS_TOKEN = "ghp_your_token_here"`,
		})
	}

	// Always suggest the filesystem server as a baseline for any project
	// (it's the most universally useful MCP server and has no prerequisites).
	// Only suggest it if there are no other suggestions yet — avoid noise for
	// projects that already have more specific options.
	//
	// TODO: consider always suggesting the filesystem server as an
	// additional entry even when other servers are detected, since it's
	// broadly useful — left as a single-suggestion fallback for now to
	// keep the generated forge.md uncluttered.
	if len(suggestions) == 0 {
		suggestions = append(suggestions, MCPSuggestion{
			Name:    "filesystem",
			Comment: "Filesystem MCP server — gives Forge read/write access to paths outside the repo",
			Block: `[[mcp.servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allow"]`,
		})
	}

	return suggestions
}

// packageJSONHasDep reads package.json and checks whether any of the given
// dep names appear in dependencies or devDependencies. Returns false if the
// file can't be read or parsed — absence is never an error here.
func packageJSONHasDep(dir string, names ...string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	for _, name := range names {
		if _, ok := pkg.Dependencies[name]; ok {
			return true
		}
		if _, ok := pkg.DevDependencies[name]; ok {
			return true
		}
	}
	return false
}

// commandExists checks if a command is available on PATH without executing it.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
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
	// scanner.Err() intentionally ignored — a read error means "not found",
	// same as a missing file (this function never reports errors).
	return false
}
