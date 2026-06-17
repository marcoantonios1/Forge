package tools

import (
	"context"
	"fmt"

	"github.com/marcoantonios1/Forge/internal/embeddings"
)

type SemanticSearchResult struct {
	Query    string                    `json:"query"`
	Results  []embeddings.SearchResult `json:"results"`
	Fallback bool                      `json:"fallback"` // true if grep fallback was used
}

func (r *SemanticSearchResult) Summary() string {
	if r.Fallback {
		return fmt.Sprintf("found %d results (grep fallback) for %q", len(r.Results), r.Query)
	}
	return fmt.Sprintf("found %d results for %q", len(r.Results), r.Query)
}

type SemanticSearchTool struct {
	client *embeddings.EmbedClient // nil = embeddings disabled, always fallback
	index  *embeddings.Index       // nil = no index built, always fallback
	root   string
}

func NewSemanticSearchTool(client *embeddings.EmbedClient, index *embeddings.Index, root string) *SemanticSearchTool {
	return &SemanticSearchTool{client: client, index: index, root: root}
}

func (t *SemanticSearchTool) Name() string { return "semantic_search" }

func (t *SemanticSearchTool) Run(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: query"}
	}

	topN := 5
	if v, ok := args["top_n"]; ok {
		switch n := v.(type) {
		case int:
			topN = n
		case float64:
			topN = int(n)
		}
	}

	if t.client == nil || t.index == nil || len(t.index.Entries) == 0 {
		return t.fallback(ctx, args, query)
	}

	results, err := embeddings.Search(ctx, t.index, t.client, query, topN)
	if err != nil {
		// Embeddings.Search failure is non-fatal — fall back to grep silently,
		// flagged via Fallback: true in the result.
		return t.fallback(ctx, args, query)
	}

	return &SemanticSearchResult{Query: query, Results: results, Fallback: false}, nil
}

func (t *SemanticSearchTool) fallback(ctx context.Context, args map[string]any, query string) (*SemanticSearchResult, error) {
	scResult, err := searchCodeFallback(ctx, args)
	if err != nil {
		return &SemanticSearchResult{Query: query, Results: nil, Fallback: true}, nil
	}

	// Deduplicate by path — one result per file, matching semantic_search output shape.
	seen := make(map[string]bool)
	results := make([]embeddings.SearchResult, 0, len(scResult.Matches))
	for _, m := range scResult.Matches {
		if !seen[m.File] {
			seen[m.File] = true
			results = append(results, embeddings.SearchResult{
				Path:  m.File,
				Score: 0,
				Text:  m.Text,
			})
		}
	}

	return &SemanticSearchResult{Query: query, Results: results, Fallback: true}, nil
}

// searchCodeFallback approximates semantic search via case-insensitive grep when
// embeddings are unavailable.
func searchCodeFallback(ctx context.Context, args map[string]any) (*SearchCodeResult, error) {
	sc := &SearchCodeTool{}
	grepArgs := map[string]any{
		"root":        args["root"],
		"pattern":     args["query"],
		"ignore_case": true,
		"max_results": 50,
	}
	res, err := sc.Run(ctx, grepArgs)
	if err != nil {
		return nil, err
	}
	return res.(*SearchCodeResult), nil
}
