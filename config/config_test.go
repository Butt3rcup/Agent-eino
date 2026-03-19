package config

import "testing"

func TestValidateAcceptsCompleteConfig(t *testing.T) {
	cfg := validConfig()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to be valid, got error: %v", err)
	}
}

func TestValidateRejectsEmptyMilvusCollectionName(t *testing.T) {
	cfg := validConfig()
	cfg.Milvus.CollectionName = ""

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty Milvus collection name to fail validation")
	}
}

func TestValidateRejectsNegativeAutoSaveMinChars(t *testing.T) {
	cfg := validConfig()
	cfg.RAG.AutoSaveMinChars = -1

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected negative auto save min chars to fail validation")
	}
}

func validConfig() *Config {
	return &Config{
		Ark: ArkConfig{
			APIKey:   "test-key",
			BaseURL:  "https://example.com",
			Model:    "test-model",
			Embedder: "test-embedder",
		},
		Milvus: MilvusConfig{
			URI:            "localhost:19530",
			DBName:         "hotwords",
			CollectionName: "hotwords_collection",
		},
		Server: ServerConfig{
			Port:               "8080",
			Host:               "127.0.0.1",
			ReadTimeoutSec:     15,
			WriteTimeoutSec:    60,
			ShutdownTimeoutSec: 10,
			CORSAllowOrigins:   []string{"*"},
		},
		Upload: UploadConfig{
			MaxSize: 1024,
			Dir:     "./uploads",
		},
		RAG: RAGConfig{
			EmbeddingDim:        1536,
			ChunkSize:           500,
			ChunkOverlap:        50,
			TopK:                5,
			MaxContextDocs:      5,
			MaxContextChars:     4000,
			MaxScoreDelta:       1,
			SimilarityThreshold: 1.5,
			AutoSaveMinChars:    120,
		},
		Security: SecurityConfig{
			RateLimitRPS:   20,
			RateLimitBurst: 40,
		},
	}
}
