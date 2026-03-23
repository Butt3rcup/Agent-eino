package vectordb

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"go.uber.org/zap"

	"go-eino-agent/pkg/logger"
)

const (
	IDField       = "id"
	ContentField  = "content"
	VectorField   = "embedding"
	MetadataField = "metadata"

	defaultFlushInterval = 800 * time.Millisecond
	defaultFlushTimeout  = 10 * time.Second
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
	client         client.Client
	dim            int
	dbName         string
	collectionName string
	flushSignals   chan struct{}
	closeCh        chan struct{}
	flushWG        sync.WaitGroup
}

func NewMilvusClient(ctx context.Context, uri, token, dbName, collectionName string, dim int) (*MilvusClient, error) {
	c, err := client.NewClient(ctx, client.Config{
		Address: uri,
		APIKey:  token,
		DBName:  dbName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Milvus: %w", err)
	}

	m := &MilvusClient{
		client:         c,
		dim:            dim,
		dbName:         dbName,
		collectionName: collectionName,
		flushSignals:   make(chan struct{}, 1),
		closeCh:        make(chan struct{}),
	}
	m.flushWG.Add(1)
	go m.flushLoop()
	return m, nil
}

func (m *MilvusClient) CreateCollection(ctx context.Context) error {
	has, err := m.client.HasCollection(ctx, m.collectionName)
	if err != nil {
		return fmt.Errorf("failed to check collection: %w", err)
	}

	if has {
		logger.Info("Milvus collection already exists",
			zap.String("db_name", m.dbName),
			zap.String("collection_name", m.collectionName),
		)
		return m.loadCollection(ctx)
	}

	schema := &entity.Schema{
		CollectionName: m.collectionName,
		AutoID:         true,
		Fields: []*entity.Field{
			{Name: IDField, DataType: entity.FieldTypeInt64, PrimaryKey: true, AutoID: true},
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
	if err := m.client.CreateIndex(ctx, m.collectionName, VectorField, idx, false); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	if err := m.loadCollection(ctx); err != nil {
		return err
	}

	logger.Info("Milvus collection created successfully",
		zap.String("db_name", m.dbName),
		zap.String("collection_name", m.collectionName),
	)
	return nil
}

func (m *MilvusClient) loadCollection(ctx context.Context) error {
	if err := m.client.LoadCollection(ctx, m.collectionName, false); err != nil {
		return fmt.Errorf("failed to load collection: %w", err)
	}
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

	if _, err := m.client.Insert(ctx, m.collectionName, "", contentCol, metadataCol, vectorCol); err != nil {
		return fmt.Errorf("failed to insert documents: %w", err)
	}
	m.scheduleFlush()
	return nil
}

func (m *MilvusClient) Search(ctx context.Context, vector []float32, topK int) ([]SearchResult, error) {
	sp, _ := entity.NewIndexHNSWSearchParam(16)

	searchResult, err := m.client.Search(
		ctx,
		m.collectionName,
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

	results := make([]SearchResult, 0, searchResult[0].ResultCount)
	for i := 0; i < searchResult[0].ResultCount; i++ {
		id, _ := searchResult[0].IDs.GetAsInt64(i)
		score := searchResult[0].Scores[i]

		var content, metadata string
		for _, field := range searchResult[0].Fields {
			switch field.Name() {
			case ContentField:
				if col, ok := field.(*entity.ColumnVarChar); ok {
					content, _ = col.ValueByIdx(i)
				}
			case MetadataField:
				if col, ok := field.(*entity.ColumnVarChar); ok {
					metadata, _ = col.ValueByIdx(i)
				}
			}
		}

		results = append(results, SearchResult{ID: id, Content: content, Metadata: metadata, Score: score})
	}
	return results, nil
}

func (m *MilvusClient) Close() {
	close(m.closeCh)
	m.flushWG.Wait()
	if m.client != nil {
		m.client.Close()
	}
}

func (m *MilvusClient) scheduleFlush() {
	select {
	case m.flushSignals <- struct{}{}:
	default:
	}
}

func (m *MilvusClient) flushLoop() {
	defer m.flushWG.Done()
	timer := time.NewTimer(defaultFlushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	pending := false

	for {
		select {
		case <-m.flushSignals:
			pending = true
			resetTimer(timer, defaultFlushInterval)
		case <-timer.C:
			if pending {
				m.flushNow()
				pending = false
			}
		case <-m.closeCh:
			if pending {
				m.flushNow()
			}
			return
		}
	}
}

func (m *MilvusClient) flushNow() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFlushTimeout)
	defer cancel()
	if err := m.client.Flush(ctx, m.collectionName, false); err != nil {
		logger.Warn("Milvus flush failed",
			zap.String("db_name", m.dbName),
			zap.String("collection_name", m.collectionName),
			zap.Error(err),
		)
	}
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}
