package embedding

import (
	"context"
	"fmt"

	arkEmbedding "github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/cloudwego/eino/components/embedding"
)

type Service struct {
	embedder embedding.Embedder
}

func NewService(apiKey, baseURL, model string) (*Service, error) {
	apiType := arkEmbedding.APITypeMultiModal
	embedder, err := arkEmbedding.NewEmbedder(context.Background(), &arkEmbedding.EmbeddingConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		APIType: &apiType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ark embedder: %w", err)
	}

	return &Service{
		embedder: embedder,
	}, nil
}

func (s *Service) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := s.embedder.EmbedStrings(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding: %w", err)
	}

	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("no embedding data returned")
	}

	// Convert float64 to float32
	result := make([]float32, len(embeddings[0]))
	for i, v := range embeddings[0] {
		result[i] = float32(v)
	}

	return result, nil
}

func (s *Service) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings, err := s.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("failed to create embeddings: %w", err)
	}

	// Convert [][]float64 to [][]float32
	result := make([][]float32, len(embeddings))
	for i, embedding := range embeddings {
		result[i] = make([]float32, len(embedding))
		for j, v := range embedding {
			result[i][j] = float32(v)
		}
	}

	return result, nil
}
