package codeintel

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
)

// Declaration is a single top-level declaration in a source file.
type Declaration struct {
	Kind      SymbolKind `json:"kind"`
	Name      string     `json:"name"`
	Line      int        `json:"line"`
	Signature string     `json:"signature,omitempty"` // best-effort; empty for regex-based languages
}

// ASTSummary is the structured result of parsing a single file.
type ASTSummary struct {
	Path         string        `json:"path"`
	Language     Language      `json:"language"`
	Declarations []Declaration `json:"declarations"`
}

// ParseFile returns a structured summary of top-level declarations in path.
// Returns ErrUnsupportedLanguage if the file's extension is not .go, .ts/.tsx, or .py.
func ParseFile(path string) (*ASTSummary, error) {
	lang := DetectLanguage(path)
	if lang == LangUnsupported {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, filepath.Ext(path))
	}

	summary := &ASTSummary{Path: path, Language: lang}

	switch lang {
	case LangGo:
		decls, err := parseFileGo(path)
		if err != nil {
			return nil, fmt.Errorf("ast_parse: go parse %s: %w", path, err)
		}
		summary.Declarations = decls

	case LangTypeScript, LangPython:
		// Heuristic/regex-based extraction — NOT a real parser. False positives/
		// negatives in edge cases (symbol in string literals, comments, etc.) are
		// an accepted v1 limitation. Signature field is left empty for these languages.
		decls, err := parseFileHeuristic(path, lang)
		if err != nil {
			return nil, fmt.Errorf("ast_parse: heuristic parse %s: %w", path, err)
		}
		summary.Declarations = decls
	}

	return summary, nil
}

// parseFileGo uses go/parser + go/ast for top-level declaration extraction.
func parseFileGo(path string) ([]Declaration, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		// Attempt to use partial results even on error.
		if f == nil {
			return nil, err
		}
	}

	var decls []Declaration

	for _, d := range f.Decls {
		switch node := d.(type) {
		case *ast.FuncDecl:
			if node.Name == nil {
				continue
			}
			kind := KindFunction
			if node.Recv != nil && len(node.Recv.List) > 0 {
				kind = KindMethod
			}
			pos := fset.Position(node.Name.Pos())
			decls = append(decls, Declaration{
				Kind:      kind,
				Name:      node.Name.Name,
				Line:      pos.Line,
				Signature: buildGoFuncSignature(node),
			})

		case *ast.GenDecl:
			switch node.Tok {
			case token.TYPE:
				for _, spec := range node.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || ts.Name == nil {
						continue
					}
					pos := fset.Position(ts.Name.Pos())
					decls = append(decls, Declaration{
						Kind: KindType,
						Name: ts.Name.Name,
						Line: pos.Line,
					})
				}
			case token.VAR, token.CONST:
				for _, spec := range node.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, ident := range vs.Names {
						pos := fset.Position(ident.Pos())
						decls = append(decls, Declaration{
							Kind: KindVariable,
							Name: ident.Name,
							Line: pos.Line,
						})
					}
				}
			}
		}
	}

	return decls, nil
}

// buildGoFuncSignature produces a best-effort signature string for a Go function.
// TODO: use go/printer.Fprint for more accurate Go function signatures
// if the current manual rendering proves insufficient in practice.
func buildGoFuncSignature(fn *ast.FuncDecl) string {
	var sb strings.Builder
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		sb.WriteString("(")
		sb.WriteString(fieldListTypeNames(fn.Recv))
		sb.WriteString(") ")
	}
	sb.WriteString(fn.Name.Name)
	sb.WriteString("(")
	if fn.Type != nil && fn.Type.Params != nil {
		sb.WriteString(fieldListTypeNames(fn.Type.Params))
	}
	sb.WriteString(")")
	if fn.Type != nil && fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		results := fieldListTypeNames(fn.Type.Results)
		if strings.Contains(results, ",") {
			sb.WriteString(" (")
			sb.WriteString(results)
			sb.WriteString(")")
		} else {
			sb.WriteString(" ")
			sb.WriteString(results)
		}
	}
	return sb.String()
}

func fieldListTypeNames(fl *ast.FieldList) string {
	if fl == nil {
		return ""
	}
	var parts []string
	for _, field := range fl.List {
		typeName := exprTypeName(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typeName)
		} else {
			for range field.Names {
				parts = append(parts, typeName)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func exprTypeName(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprTypeName(t.X)
	case *ast.SelectorExpr:
		return exprTypeName(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprTypeName(t.Elt)
	case *ast.MapType:
		return "map[" + exprTypeName(t.Key) + "]" + exprTypeName(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + exprTypeName(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

type declPattern struct {
	re   *regexp.Regexp
	kind SymbolKind
}

func buildDeclPatterns(lang Language) []declPattern {
	var patterns []declPattern

	switch lang {
	case LangTypeScript:
		patterns = []declPattern{
			{regexp.MustCompile(`^\s*(export\s+)?(async\s+)?function\s+(\w+)\b`), KindFunction},
			{regexp.MustCompile(`^\s*(export\s+)?(default\s+)?class\s+(\w+)\b`), KindClass},
			{regexp.MustCompile(`^\s*(export\s+)?interface\s+(\w+)\b`), KindInterface},
			{regexp.MustCompile(`^\s*(export\s+)?(const|let|var)\s+(\w+)\s*=`), KindVariable},
			{regexp.MustCompile(`^\s*(public|private|protected)?\s*(\w+)\s*\(`), KindMethod},
		}
	case LangPython:
		patterns = []declPattern{
			{regexp.MustCompile(`^\s*def\s+(\w+)\s*\(`), KindFunction},
			{regexp.MustCompile(`^\s*class\s+(\w+)\b`), KindClass},
			{regexp.MustCompile(`^\s*(\w+)\s*=`), KindVariable},
		}
	}

	return patterns
}

// nameGroupIndex returns the index of the capture group that holds the symbol name.
// The patterns above have the name in different group positions depending on prefix groups.
func extractNameFromMatch(m []string, lang Language, patIdx int) string {
	// Find the last non-empty group — the name is always the last substantive capture.
	for i := len(m) - 1; i >= 1; i-- {
		if s := m[i]; s != "" && !isKeyword(s) {
			return s
		}
	}
	return ""
}

var goKeywords = map[string]bool{
	"export": true, "default": true, "async": true, "function": true,
	"class": true, "interface": true, "const": true, "let": true, "var": true,
	"public": true, "private": true, "protected": true,
	"def": true,
}

func isKeyword(s string) bool {
	return goKeywords[s]
}

func parseFileHeuristic(path string, lang Language) ([]Declaration, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	patterns := buildDeclPatterns(lang)
	var decls []Declaration

	for i, line := range lines {
		for _, p := range patterns {
			m := p.re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := extractNameFromMatch(m, lang, 0)
			if name == "" {
				continue
			}
			decls = append(decls, Declaration{
				Kind: p.kind,
				Name: name,
				Line: i + 1,
				// Signature intentionally empty for regex-based languages.
			})
			break // only match one pattern per line
		}
	}

	return decls, nil
}
