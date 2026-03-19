package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Ark      ArkConfig
	Milvus   MilvusConfig
	Server   ServerConfig
	Upload   UploadConfig
	RAG      RAGConfig
	Ollama   OllamaConfig
	Security SecurityConfig
}

type ArkConfig struct {
	APIKey   string
	BaseURL  string
	Region   string
	Model    string
	Embedder string
}

type OllamaConfig struct {
	BaseURL string
	Model   string
}

type MilvusConfig struct {
	URI            string
	Token          string
	DBName         string
	CollectionName string
}

type ServerConfig struct {
	Port               string
	Host               string
	ReadTimeoutSec     int
	WriteTimeoutSec    int
	ShutdownTimeoutSec int
	TrustedProxies     []string
	CORSAllowOrigins   []string
}

type UploadConfig struct {
	MaxSize int64
	Dir     string
}

type RAGConfig struct {
	EmbeddingModel  string
	EmbeddingDim    int
	ChunkSize       int
	ChunkOverlap    int
	TopK            int
	MaxContextDocs  int
	MaxContextChars int
	MaxScoreDelta   float32

	// 智能知识增强配置
	EnableAutoSearch     bool    // 是否启用自动联网搜索
	SimilarityThreshold  float32 // 相似度阈值（L2距离，越小越相似）
	AutoSaveSearchResult bool    // 是否自动保存搜索结果到知识库
	AutoSaveMinChars     int     // 自动保存最小内容长度
}

type SecurityConfig struct {
	RateLimitRPS   float64
	RateLimitBurst float64
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
			URI:            getEnv("MILVUS_URI", "localhost:19530"),
			Token:          getEnv("MILVUS_TOKEN", ""),
			DBName:         getEnv("MILVUS_DB_NAME", "hotwords"),
			CollectionName: getEnv("MILVUS_COLLECTION_NAME", "hotwords_collection"),
		},
		Server: ServerConfig{
			Port:               getEnv("SERVER_PORT", "8080"),
			Host:               getEnv("SERVER_HOST", "0.0.0.0"),
			ReadTimeoutSec:     getEnvInt("SERVER_READ_TIMEOUT_SEC", 15),
			WriteTimeoutSec:    getEnvInt("SERVER_WRITE_TIMEOUT_SEC", 60),
			ShutdownTimeoutSec: getEnvInt("SERVER_SHUTDOWN_TIMEOUT_SEC", 10),
			TrustedProxies:     getEnvCSV("TRUSTED_PROXIES", ""),
			CORSAllowOrigins:   getEnvCSV("CORS_ALLOWED_ORIGINS", "*"),
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
			MaxContextDocs:       getEnvInt("MAX_CONTEXT_DOCS", 5),
			MaxContextChars:      getEnvInt("MAX_CONTEXT_CHARS", 4000),
			MaxScoreDelta:        getFloat32Env("MAX_SCORE_DELTA", 1.0),
			EnableAutoSearch:     getBoolEnv("ENABLE_AUTO_SEARCH", true),
			SimilarityThreshold:  getFloat32Env("SIMILARITY_THRESHOLD", 1.5),
			AutoSaveSearchResult: getBoolEnv("AUTO_SAVE_SEARCH_RESULT", true),
			AutoSaveMinChars:     getEnvInt("AUTO_SAVE_MIN_CHARS", 120),
		},
		Ollama: OllamaConfig{
			BaseURL: getEnv("OLLAMA_BASE_URL", ""),
			Model:   getEnv("OLLAMA_MODEL", ""),
		},
		Security: SecurityConfig{
			RateLimitRPS:   getEnvFloat("RATE_LIMIT_RPS", 20),
			RateLimitBurst: getEnvFloat("RATE_LIMIT_BURST", 40),
		},
	}
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.Ark.APIKey) == "" {
		return fmt.Errorf("ARK_API_KEY is required")
	}
	if strings.TrimSpace(c.Ark.Model) == "" {
		return fmt.Errorf("MODEL is required")
	}
	if strings.TrimSpace(c.Ark.Embedder) == "" {
		return fmt.Errorf("EMBEDDER is required")
	}
	if strings.TrimSpace(c.Milvus.URI) == "" {
		return fmt.Errorf("MILVUS_URI is required")
	}
	if strings.TrimSpace(c.Milvus.DBName) == "" {
		return fmt.Errorf("MILVUS_DB_NAME is required")
	}
	if strings.TrimSpace(c.Milvus.CollectionName) == "" {
		return fmt.Errorf("MILVUS_COLLECTION_NAME is required")
	}
	port, err := strconv.Atoi(c.Server.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SERVER_PORT must be in range 1-65535")
	}
	if c.Server.ReadTimeoutSec <= 0 || c.Server.WriteTimeoutSec <= 0 || c.Server.ShutdownTimeoutSec <= 0 {
		return fmt.Errorf("server timeout values must be positive")
	}
	if c.Upload.MaxSize <= 0 {
		return fmt.Errorf("MAX_UPLOAD_SIZE must be positive")
	}
	if strings.TrimSpace(c.Upload.Dir) == "" {
		return fmt.Errorf("UPLOAD_DIR is required")
	}
	if c.RAG.EmbeddingDim <= 0 {
		return fmt.Errorf("EMBEDDING_DIM must be positive")
	}
	if c.RAG.ChunkSize <= 0 {
		return fmt.Errorf("CHUNK_SIZE must be positive")
	}
	if c.RAG.ChunkOverlap < 0 || c.RAG.ChunkOverlap >= c.RAG.ChunkSize {
		return fmt.Errorf("CHUNK_OVERLAP must be >=0 and < CHUNK_SIZE")
	}
	if c.RAG.TopK <= 0 || c.RAG.TopK > 50 {
		return fmt.Errorf("TOP_K must be in range 1-50")
	}
	if c.RAG.MaxContextDocs <= 0 || c.RAG.MaxContextDocs > 50 {
		return fmt.Errorf("MAX_CONTEXT_DOCS must be in range 1-50")
	}
	if c.RAG.MaxContextChars <= 200 {
		return fmt.Errorf("MAX_CONTEXT_CHARS must be greater than 200")
	}
	if c.RAG.MaxScoreDelta <= 0 {
		return fmt.Errorf("MAX_SCORE_DELTA must be positive")
	}
	if c.RAG.SimilarityThreshold <= 0 {
		return fmt.Errorf("SIMILARITY_THRESHOLD must be positive")
	}
	if c.RAG.AutoSaveMinChars < 0 {
		return fmt.Errorf("AUTO_SAVE_MIN_CHARS must be non-negative")
	}
	if c.Security.RateLimitRPS <= 0 || c.Security.RateLimitBurst < 1 {
		return fmt.Errorf("rate limit config must be positive")
	}
	if len(c.Server.CORSAllowOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS must not be empty")
	}
	return nil
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

func getEnvCSV(key, defaultValue string) []string {
	raw := getEnv(key, defaultValue)
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}
