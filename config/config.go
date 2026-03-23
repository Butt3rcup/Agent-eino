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
	MaxSize       int64
	Dir           string
	PDFEnabled    bool
	TaskQueueSize int
	TaskWorkers   int
}

type RAGConfig struct {
	EmbeddingModel        string
	EmbeddingDim          int
	ChunkSize             int
	ChunkOverlap          int
	TopK                  int
	MaxContextDocs        int
	MaxContextChars       int
	MaxScoreDelta         float32
	EnableAutoSearch      bool
	SimilarityThreshold   float32
	AutoSaveSearchResult  bool
	AutoSaveMinChars      int
	AsyncKnowledgePersist bool
	PersistQueueSize      int
	QueryCacheSize        int
	QueryCacheTTLSeconds  int
}

type SecurityConfig struct {
	RateLimitRPS               float64
	RateLimitBurst             float64
	QueryDefaultTimeoutSec     int
	QueryDefaultRateLimitRPS   float64
	QueryDefaultRateLimitBurst float64
	QueryModeTimeoutSec        map[string]int
	QueryModeRateLimitRPS      map[string]float64
	QueryModeRateLimitBurst    map[string]float64
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
			MaxSize:       getEnvInt64("MAX_UPLOAD_SIZE", 10485760),
			Dir:           getEnv("UPLOAD_DIR", "./uploads"),
			PDFEnabled:    getBoolEnv("PDF_UPLOAD_ENABLED", false),
			TaskQueueSize: getEnvInt("UPLOAD_TASK_QUEUE_SIZE", 8),
			TaskWorkers:   getEnvInt("UPLOAD_TASK_WORKERS", 2),
		},
		RAG: RAGConfig{
			EmbeddingModel:        getEnv("EMBEDDING_MODEL", "text-embedding-3-small"),
			EmbeddingDim:          getEnvInt("EMBEDDING_DIM", 1536),
			ChunkSize:             getEnvInt("CHUNK_SIZE", 500),
			ChunkOverlap:          getEnvInt("CHUNK_OVERLAP", 50),
			TopK:                  getEnvInt("TOP_K", 5),
			MaxContextDocs:        getEnvInt("MAX_CONTEXT_DOCS", 5),
			MaxContextChars:       getEnvInt("MAX_CONTEXT_CHARS", 4000),
			MaxScoreDelta:         getFloat32Env("MAX_SCORE_DELTA", 1.0),
			EnableAutoSearch:      getBoolEnv("ENABLE_AUTO_SEARCH", true),
			SimilarityThreshold:   getFloat32Env("SIMILARITY_THRESHOLD", 1.5),
			AutoSaveSearchResult:  getBoolEnv("AUTO_SAVE_SEARCH_RESULT", true),
			AutoSaveMinChars:      getEnvInt("AUTO_SAVE_MIN_CHARS", 120),
			AsyncKnowledgePersist: getBoolEnv("ASYNC_KNOWLEDGE_PERSIST", true),
			PersistQueueSize:      getEnvInt("PERSIST_QUEUE_SIZE", 16),
			QueryCacheSize:        getEnvInt("QUERY_CACHE_SIZE", 256),
			QueryCacheTTLSeconds:  getEnvInt("QUERY_CACHE_TTL_SECONDS", 30),
		},
		Ollama: OllamaConfig{
			BaseURL: getEnv("OLLAMA_BASE_URL", ""),
			Model:   getEnv("OLLAMA_MODEL", ""),
		},
		Security: SecurityConfig{
			RateLimitRPS:               getEnvFloat("RATE_LIMIT_RPS", 20),
			RateLimitBurst:             getEnvFloat("RATE_LIMIT_BURST", 40),
			QueryDefaultTimeoutSec:     getEnvInt("QUERY_DEFAULT_TIMEOUT_SEC", 45),
			QueryDefaultRateLimitRPS:   getEnvFloat("QUERY_DEFAULT_RATE_LIMIT_RPS", 8),
			QueryDefaultRateLimitBurst: getEnvFloat("QUERY_DEFAULT_RATE_LIMIT_BURST", 16),
			QueryModeTimeoutSec:        getEnvIntMap("QUERY_MODE_TIMEOUTS"),
			QueryModeRateLimitRPS:      getEnvFloatMap("QUERY_MODE_RATE_LIMIT_RPS"),
			QueryModeRateLimitBurst:    getEnvFloatMap("QUERY_MODE_RATE_LIMIT_BURST"),
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
	if c.Upload.TaskQueueSize <= 0 {
		return fmt.Errorf("UPLOAD_TASK_QUEUE_SIZE must be positive")
	}
	if c.Upload.TaskWorkers <= 0 {
		return fmt.Errorf("UPLOAD_TASK_WORKERS must be positive")
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
	if c.RAG.PersistQueueSize <= 0 {
		return fmt.Errorf("PERSIST_QUEUE_SIZE must be positive")
	}
	if c.RAG.QueryCacheSize < 0 {
		return fmt.Errorf("QUERY_CACHE_SIZE must be non-negative")
	}
	if c.RAG.QueryCacheTTLSeconds < 0 {
		return fmt.Errorf("QUERY_CACHE_TTL_SECONDS must be non-negative")
	}
	if c.Security.RateLimitRPS <= 0 || c.Security.RateLimitBurst < 1 {
		return fmt.Errorf("rate limit config must be positive")
	}
	if c.Security.QueryDefaultTimeoutSec <= 0 {
		return fmt.Errorf("QUERY_DEFAULT_TIMEOUT_SEC must be positive")
	}
	if c.Security.QueryDefaultRateLimitRPS <= 0 || c.Security.QueryDefaultRateLimitBurst < 1 {
		return fmt.Errorf("default query mode rate limit config must be positive")
	}
	if err := validatePositiveIntMap("QUERY_MODE_TIMEOUTS", c.Security.QueryModeTimeoutSec); err != nil {
		return err
	}
	if err := validatePositiveFloatMap("QUERY_MODE_RATE_LIMIT_RPS", c.Security.QueryModeRateLimitRPS); err != nil {
		return err
	}
	if err := validatePositiveFloatMap("QUERY_MODE_RATE_LIMIT_BURST", c.Security.QueryModeRateLimitBurst); err != nil {
		return err
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

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

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
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	return values
}

func getEnvIntMap(key string) map[string]int {
	values := make(map[string]int)
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return values
	}
	for _, part := range strings.Split(raw, ",") {
		name, value, ok := splitModePair(part)
		if !ok {
			log.Printf("Warning: invalid %s entry %q, expected mode:value", key, part)
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("Warning: invalid %s value %q for mode %s", key, value, name)
			continue
		}
		values[name] = parsed
	}
	return values
}

func getEnvFloatMap(key string) map[string]float64 {
	values := make(map[string]float64)
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return values
	}
	for _, part := range strings.Split(raw, ",") {
		name, value, ok := splitModePair(part)
		if !ok {
			log.Printf("Warning: invalid %s entry %q, expected mode:value", key, part)
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			log.Printf("Warning: invalid %s value %q for mode %s", key, value, name)
			continue
		}
		values[name] = parsed
	}
	return values
}

func splitModePair(raw string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	name := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if name == "" || value == "" {
		return "", "", false
	}
	return name, value, true
}

func validatePositiveIntMap(name string, values map[string]int) error {
	for mode, value := range values {
		if strings.TrimSpace(mode) == "" {
			return fmt.Errorf("%s contains an empty mode name", name)
		}
		if value <= 0 {
			return fmt.Errorf("%s for mode %s must be positive", name, mode)
		}
	}
	return nil
}

func validatePositiveFloatMap(name string, values map[string]float64) error {
	for mode, value := range values {
		if strings.TrimSpace(mode) == "" {
			return fmt.Errorf("%s contains an empty mode name", name)
		}
		if value <= 0 {
			return fmt.Errorf("%s for mode %s must be positive", name, mode)
		}
	}
	return nil
}
