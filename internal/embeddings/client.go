package embeddings

import (
	"context"
	"errors"

	"github.com/marcoantonios1/Forge/internal/costguard"
)

type EmbedClient struct {
	cg    *costguard.Client
	model string
}

func NewEmbedClient(cg *costguard.Client, model string) *EmbedClient {
	return &EmbedClient{cg: cg, model: model}
}

// Embed sends one text input to Costguard's /v1/embeddings and returns the vector.
func (e *EmbedClient) Embed(ctx context.Context, text string) ([]float64, error) {
	resp, err := e.cg.Embed(ctx, costguard.EmbeddingRequest{
		Model: e.model,
		Input: text,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("embeddings: empty response")
	}
	return resp.Data[0].Embedding, nil
}
