package codeintel

import (
	"errors"
	"path/filepath"
)

// Language represents a supported source language.
type Language string

const (
	LangGo          Language = "go"
	LangTypeScript  Language = "typescript"
	LangPython      Language = "python"
	LangUnsupported Language = "unsupported"
)

// DetectLanguage maps a file extension to a supported Language.
func DetectLanguage(path string) Language {
	switch filepath.Ext(path) {
	case ".go":
		return LangGo
	case ".ts", ".tsx":
		return LangTypeScript
	case ".py":
		return LangPython
	default:
		return LangUnsupported
	}
}

var extsByLanguage = map[Language]map[string]bool{
	LangGo:         {".go": true},
	LangTypeScript: {".ts": true, ".tsx": true},
	LangPython:     {".py": true},
}

// ErrUnsupportedLanguage is returned by analysis functions when a file's
// language has no parser implementation.
var ErrUnsupportedLanguage = errors.New("codeintel: unsupported language")

// TODO: add support for Java, Rust, C/C++ — forgeinit.go already detects
// these languages; codeintel intentionally scopes to Go/TypeScript/Python for v1.
