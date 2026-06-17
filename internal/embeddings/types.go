package embeddings

import "time"

type Chunk struct {
	Path  string `json:"path"`  // relative path from repo root
	Index int    `json:"index"` // chunk number within the file (0-based)
	Text  string `json:"text"`  // the chunk's raw text content
	Hash  string `json:"hash"`  // sha256 of Text, for change detection
}

type IndexEntry struct {
	Chunk  Chunk     `json:"chunk"`
	Vector []float64 `json:"vector"`
}

type Index struct {
	Model      string            `json:"model"`       // embedding model used to build this index
	BuiltAt    time.Time         `json:"built_at"`
	Entries    []IndexEntry      `json:"entries"`
	FileHashes map[string]string `json:"file_hashes"` // path → sha256 of full file content
}
