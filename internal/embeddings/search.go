package embeddings

import (
	"context"
	"math"
	"sort"
)

type SearchResult struct {
	Path  string  `json:"path"`
	Score float64 `json:"score"` // cosine similarity, 0..1
	Text  string  `json:"text"`  // the matching chunk's text (for context)
}

// Search embeds the query and returns the top N files by best-matching chunk
// score, deduplicated by path (one result per file — its highest-scoring chunk).
func Search(ctx context.Context, idx *Index, client *EmbedClient, query string, topN int) ([]SearchResult, error) {
	queryVec, err := client.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	best := make(map[string]SearchResult)
	for _, entry := range idx.Entries {
		score := cosineSimilarity(queryVec, entry.Vector)
		if existing, ok := best[entry.Chunk.Path]; !ok || score > existing.Score {
			best[entry.Chunk.Path] = SearchResult{
				Path:  entry.Chunk.Path,
				Score: score,
				Text:  entry.Chunk.Text,
			}
		}
	}

	results := make([]SearchResult, 0, len(best))
	for _, r := range best {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topN > 0 && len(results) > topN {
		results = results[:topN]
	}
	return results, nil
}

// cosineSimilarity computes the cosine similarity between two equal-length
// float64 vectors. Returns 0 if either vector is empty or lengths mismatch.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
