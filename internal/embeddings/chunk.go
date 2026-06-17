package embeddings

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
)

const maxChunkChars = 2000

// ChunkFile splits file content into chunks of at most maxChunkChars,
// breaking on line boundaries where possible.
func ChunkFile(path, content string) []Chunk {
	if content == "" {
		return nil
	}
	if len(content) <= maxChunkChars {
		h := sha256.Sum256([]byte(content))
		return []Chunk{{
			Path:  path,
			Index: 0,
			Text:  content,
			Hash:  fmt.Sprintf("%x", h),
		}}
	}

	lines := strings.Split(content, "\n")
	var chunks []Chunk
	var current strings.Builder
	chunkIdx := 0

	for _, line := range lines {
		lineWithNewline := line + "\n"
		if current.Len() > 0 && current.Len()+len(lineWithNewline) > maxChunkChars {
			text := current.String()
			h := sha256.Sum256([]byte(text))
			chunks = append(chunks, Chunk{
				Path:  path,
				Index: chunkIdx,
				Text:  text,
				Hash:  fmt.Sprintf("%x", h),
			})
			chunkIdx++
			current.Reset()
		}
		current.WriteString(lineWithNewline)
	}

	if current.Len() > 0 {
		text := current.String()
		h := sha256.Sum256([]byte(text))
		chunks = append(chunks, Chunk{
			Path:  path,
			Index: chunkIdx,
			Text:  text,
			Hash:  fmt.Sprintf("%x", h),
		})
	}

	return chunks
}

var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".pdf": true, ".zip": true, ".tar": true,
	".gz": true, ".exe": true, ".bin": true, ".so": true,
	".dylib": true, ".dll": true, ".woff": true, ".woff2": true,
	".ttf": true, ".eot": true, ".webp": true, ".svg": true,
	".mp4": true, ".mp3": true, ".wav": true, ".avi": true,
}

var excludedFilenames = map[string]bool{
	"go.sum":            true,
	"package-lock.json": true,
	"yarn.lock":         true,
	"Cargo.lock":        true,
}

// ShouldIndex returns true if the file should be indexed for semantic search.
// Excludes binary files, known lock files, and minified assets.
func ShouldIndex(path string) bool {
	base := filepath.Base(path)
	if excludedFilenames[base] {
		return false
	}
	if strings.HasSuffix(base, ".min.js") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != "" && binaryExtensions[ext] {
		return false
	}
	return true
}
