package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Ark      ArkConfig
	Milvus   MilvusConfig
	Server   ServerConfig
	Upload   UploadConfig
	RAG      RAGConfig
	LLM      LLMConfig
}

type ArkConfig struct {
	APIKey   string
	BaseURL  string
	Region   string
	Model    string
	Embedder string
}

type MilvusConfig struct {
	URI    string
	Token  string
	DBName string
}

type ServerConfig struct {
	Port string
	Host string
}

type UploadConfig struct {
	MaxSize int64
	Dir     string
}

type RAGConfig struct {
	EmbeddingModel string
	EmbeddingDim   int
	ChunkSize      int
	ChunkOverlap   int
	TopK           int

	// 智能知识增强配置
	EnableAutoSearch     bool    // 是否启用自动联网搜索
	SimilarityThreshold  float32 // 相似度阈值（L2距离，越小越相似）
	AutoSaveSearchResult bool    // 是否自动保存搜索结果到知识库
}

type LLMConfig struct {
	Model       string
	Temperature float64
	MaxTokens   int
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using environment variables")
	}

	return &Config{
		Ark: ArkConfig{
			APIKey:   getEnv("ARK_API_KEY", ""),
			BaseURL:  getEnv("ARK_BASE_URL", "https://ark.cn-beijing.volces.com/api/v3"),
			Region:   getEnv("ARK_REGION", "cn-beijing"),
			Model:    getEnv("MODEL", "doubao-1-5-pro-32k-250115"),
			Embedder: getEnv("EMBEDDER", ""),
		},
		Milvus: MilvusConfig{
			URI:    getEnv("MILVUS_URI", "localhost:19530"),
			Token:  getEnv("MILVUS_TOKEN", ""),
			DBName: getEnv("MILVUS_DB_NAME", "hotwords"),
		},
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
		},
		Upload: UploadConfig{
			MaxSize: getEnvInt64("MAX_UPLOAD_SIZE", 10485760),
			Dir:     getEnv("UPLOAD_DIR", "./uploads"),
		},
		RAG: RAGConfig{
			EmbeddingModel:       getEnv("EMBEDDING_MODEL", "text-embedding-3-small"),
			EmbeddingDim:         getEnvInt("EMBEDDING_DIM", 1536),
			ChunkSize:            getEnvInt("CHUNK_SIZE", 500),
			ChunkOverlap:         getEnvInt("CHUNK_OVERLAP", 50),
			TopK:                 getEnvInt("TOP_K", 5),
			EnableAutoSearch:     getBoolEnv("ENABLE_AUTO_SEARCH", true),
			SimilarityThreshold:  getFloat32Env("SIMILARITY_THRESHOLD", 1.5),
			AutoSaveSearchResult: getBoolEnv("AUTO_SAVE_SEARCH_RESULT", true),
		},
		LLM: LLMConfig{
			Model:       getEnv("LLM_MODEL", "gpt-4o-mini"),
			Temperature: getEnvFloat("LLM_TEMPERATURE", 0.7),
			MaxTokens:   getEnvInt("LLM_MAX_TOKENS", 2000),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultValue
}

// getBoolEnv 获取布尔类型环境变量
func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

// getFloat32Env 获取 float32 类型环境变量
func getFloat32Env(key string, defaultValue float32) float32 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 32); err == nil {
			return float32(f)
		}
	}
	return defaultValue
}
