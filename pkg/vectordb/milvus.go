package vectordb

import (
	"context"
	"fmt"
	"log"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	CollectionName = "hotwords_collection"
	IDField        = "id"
	ContentField   = "content"
	VectorField    = "embedding"
	MetadataField  = "metadata"
)

type Document struct {
	ID       int64
	Content  string
	Metadata string
	Vector   []float32
}

type SearchResult struct {
	ID       int64
	Content  string
	Metadata string
	Score    float32
}

type MilvusClient struct {
	client client.Client
	dim    int
}

func NewMilvusClient(uri, token string, dim int) (*MilvusClient, error) {
	c, err := client.NewClient(context.Background(), client.Config{
		Address: uri,
		APIKey:  token,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Milvus: %w", err)
	}

	return &MilvusClient{
		client: c,
		dim:    dim,
	}, nil
}

func (m *MilvusClient) CreateCollection(ctx context.Context) error {
	has, err := m.client.HasCollection(ctx, CollectionName)
	if err != nil {
		return fmt.Errorf("failed to check collection: %w", err)
	}

	if has {
		log.Printf("Collection %s already exists", CollectionName)
		return nil
	}

	schema := &entity.Schema{
		CollectionName: CollectionName,
		AutoID:         true,
		Fields: []*entity.Field{
			{
				Name:       IDField,
				DataType:   entity.FieldTypeInt64,
				PrimaryKey: true,
				AutoID:     true,
			},
			{
				Name:     ContentField,
				DataType: entity.FieldTypeVarChar,
				TypeParams: map[string]string{
					"max_length": "65535",
				},
			},
			{
				Name:     MetadataField,
				DataType: entity.FieldTypeVarChar,
				TypeParams: map[string]string{
					"max_length": "1024",
				},
			},
			{
				Name:     VectorField,
				DataType: entity.FieldTypeFloatVector,
				TypeParams: map[string]string{
					"dim": fmt.Sprintf("%d", m.dim),
				},
			},
		},
	}

	if err := m.client.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	idx, err := entity.NewIndexHNSW(entity.L2, 8, 200)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	if err := m.client.CreateIndex(ctx, CollectionName, VectorField, idx, false); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	if err := m.client.LoadCollection(ctx, CollectionName, false); err != nil {
		return fmt.Errorf("failed to load collection: %w", err)
	}

	log.Printf("Collection %s created successfully", CollectionName)
	return nil
}

func (m *MilvusClient) Insert(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	contents := make([]string, len(docs))
	metadatas := make([]string, len(docs))
	vectors := make([][]float32, len(docs))

	for i, doc := range docs {
		contents[i] = doc.Content
		metadatas[i] = doc.Metadata
		vectors[i] = doc.Vector
	}

	contentCol := entity.NewColumnVarChar(ContentField, contents)
	metadataCol := entity.NewColumnVarChar(MetadataField, metadatas)
	vectorCol := entity.NewColumnFloatVector(VectorField, m.dim, vectors)

	if _, err := m.client.Insert(ctx, CollectionName, "", contentCol, metadataCol, vectorCol); err != nil {
		return fmt.Errorf("failed to insert documents: %w", err)
	}

	// 异步 Flush，避免每次插入都同步阻塞等待刷盘
	go func() {
		if err := m.client.Flush(context.Background(), CollectionName, false); err != nil {
			log.Printf("[Milvus] async flush failed: %v", err)
		}
	}()

	return nil
}

func (m *MilvusClient) Search(ctx context.Context, vector []float32, topK int) ([]SearchResult, error) {
	sp, _ := entity.NewIndexHNSWSearchParam(16)

	searchResult, err := m.client.Search(
		ctx,
		CollectionName,
		[]string{},
		"",
		[]string{ContentField, MetadataField},
		[]entity.Vector{entity.FloatVector(vector)},
		VectorField,
		entity.L2,
		topK,
		sp,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	if len(searchResult) == 0 {
		return []SearchResult{}, nil
	}

	results := make([]SearchResult, 0)
	for i := 0; i < searchResult[0].ResultCount; i++ {
		id, _ := searchResult[0].IDs.GetAsInt64(i)
		score := searchResult[0].Scores[i]

		var content, metadata string
		for _, field := range searchResult[0].Fields {
			if field.Name() == ContentField {
				if col, ok := field.(*entity.ColumnVarChar); ok {
					content, _ = col.ValueByIdx(i)
				}
			}
			if field.Name() == MetadataField {
				if col, ok := field.(*entity.ColumnVarChar); ok {
					metadata, _ = col.ValueByIdx(i)
				}
			}
		}

		results = append(results, SearchResult{
			ID:       id,
			Content:  content,
			Metadata: metadata,
			Score:    score,
		})
	}

	return results, nil
}

func (m *MilvusClient) Close() {
	if m.client != nil {
		m.client.Close()
	}
}
