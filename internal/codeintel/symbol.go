package codeintel

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
)

// SymbolKind classifies what kind of declaration a symbol is.
type SymbolKind string

const (
	KindFunction  SymbolKind = "function"
	KindMethod    SymbolKind = "method"
	KindType      SymbolKind = "type"
	KindVariable  SymbolKind = "variable"
	KindClass     SymbolKind = "class"     // Python/TS
	KindInterface SymbolKind = "interface" // TS
)

// SymbolOccurrence is a single location where a symbol appears.
type SymbolOccurrence struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"` // trimmed source line, for context
}

// SymbolResult holds all definitions and references found for a symbol.
type SymbolResult struct {
	Name        string             `json:"name"`
	Definitions []SymbolOccurrence `json:"definitions"`
	References  []SymbolOccurrence `json:"references"`
}

// FindSymbol searches all supported-language files under root for definitions
// and references of name. Unsupported-language files are silently skipped.
func FindSymbol(root, name string) (*SymbolResult, error) {
	allExts := map[string]bool{}
	for _, exts := range extsByLanguage {
		for ext := range exts {
			allExts[ext] = true
		}
	}

	files, err := WalkFiles(root, allExts)
	if err != nil {
		return nil, fmt.Errorf("symbol_lookup: walk: %w", err)
	}

	result := &SymbolResult{Name: name}

	for _, rel := range files {
		absPath := root + string(os.PathSeparator) + rel
		lang := DetectLanguage(rel)

		switch lang {
		case LangGo:
			defs, refs, err := findSymbolGo(absPath, rel, name)
			if err != nil {
				continue // skip unparseable files
			}
			result.Definitions = append(result.Definitions, defs...)
			result.References = append(result.References, refs...)

		case LangTypeScript, LangPython:
			defs, refs, err := findSymbolHeuristic(absPath, rel, name, lang)
			if err != nil {
				continue
			}
			result.Definitions = append(result.Definitions, defs...)
			result.References = append(result.References, refs...)
		}
	}

	return result, nil
}

// findSymbolGo uses go/parser + go/ast for precise definition/reference detection.
func findSymbolGo(absPath, relPath, name string) (defs, refs []SymbolOccurrence, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, 0)
	if err != nil {
		return nil, nil, err
	}

	// Read source lines for Text field.
	lines, err := readLines(absPath)
	if err != nil {
		return nil, nil, err
	}

	// Collect definition positions first.
	defPositions := map[token.Pos]bool{}

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Name != nil && node.Name.Name == name {
				defPositions[node.Name.Pos()] = true
			}
		case *ast.TypeSpec:
			if node.Name != nil && node.Name.Name == name {
				defPositions[node.Name.Pos()] = true
			}
		case *ast.ValueSpec:
			for _, ident := range node.Names {
				if ident.Name == name {
					defPositions[ident.Pos()] = true
				}
			}
		}
		return true
	})

	// Second pass: classify all identifiers matching name.
	ast.Inspect(f, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Name != name {
			return true
		}
		pos := fset.Position(ident.Pos())
		lineIdx := pos.Line - 1
		var text string
		if lineIdx >= 0 && lineIdx < len(lines) {
			text = strings.TrimSpace(lines[lineIdx])
		}
		occ := SymbolOccurrence{Path: relPath, Line: pos.Line, Text: text}
		if defPositions[ident.Pos()] {
			defs = append(defs, occ)
		} else {
			refs = append(refs, occ)
		}
		return true
	})

	return defs, refs, nil
}

// findSymbolHeuristic uses regex patterns for TypeScript and Python.
// This is heuristic/regex-based and NOT a real parser — false positives/negatives
// in edge cases (e.g. symbol name inside string literals or comments) are an
// accepted v1 limitation.
func findSymbolHeuristic(absPath, relPath, name string, lang Language) (defs, refs []SymbolOccurrence, err error) {
	defPatterns := buildDefPatterns(name, lang)
	refRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)

	lines, err := readLines(absPath)
	if err != nil {
		return nil, nil, err
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)
		isDef := false
		for _, re := range defPatterns {
			if re.MatchString(line) {
				defs = append(defs, SymbolOccurrence{Path: relPath, Line: lineNum, Text: trimmed})
				isDef = true
				break
			}
		}
		if !isDef && refRe.MatchString(line) {
			refs = append(refs, SymbolOccurrence{Path: relPath, Line: lineNum, Text: trimmed})
		}
	}

	return defs, refs, nil
}

func buildDefPatterns(name string, lang Language) []*regexp.Regexp {
	q := regexp.QuoteMeta(name)
	var rawPatterns []string

	switch lang {
	case LangTypeScript:
		rawPatterns = []string{
			`^\s*(export\s+)?(async\s+)?function\s+` + q + `\b`,
			`^\s*(export\s+)?(default\s+)?class\s+` + q + `\b`,
			`^\s*(export\s+)?interface\s+` + q + `\b`,
			`^\s*(export\s+)?(const|let|var)\s+` + q + `\s*=`,
			`^\s*(public|private|protected)?\s*` + q + `\s*\(`,
		}
	case LangPython:
		rawPatterns = []string{
			`^\s*def\s+` + q + `\s*\(`,
			`^\s*class\s+` + q + `\b`,
			`^\s*` + q + `\s*=`,
		}
	}

	patterns := make([]*regexp.Regexp, 0, len(rawPatterns))
	for _, raw := range rawPatterns {
		if re, err := regexp.Compile(raw); err == nil {
			patterns = append(patterns, re)
		}
	}
	return patterns
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
