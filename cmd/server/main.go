package main

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"go-eino-agent/config"
	"go-eino-agent/internal/handler"
	"go-eino-agent/pkg/logger"
)

func main() {
	// 初始化日志系统
	if err := logger.Init(); err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
	defer logger.Sync()

	// 加载配置
	cfg := config.Load()

	// 验证必要的配置
	if cfg.Ark.APIKey == "" {
		logger.Fatal("ARK_API_KEY is required")
	}

	// 创建 Handler
	h, err := handler.NewHandler(cfg)
	if err != nil {
		logger.Fatal("Failed to create handler", zap.Error(err))
	}
	defer h.Close()

	// 创建 Gin 路由
	r := gin.Default()

	// CORS 中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 静态文件服务
	r.Static("/static", "./web/static")
	r.LoadHTMLGlob("web/templates/*")

	// 首页
	r.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.html", gin.H{
			"title": "网络热词 RAG 系统",
		})
	})

	// API 路由
	api := r.Group("/api")
	{
		api.GET("/health", h.HandleHealth)
		api.POST("/upload", h.HandleUpload)
		api.POST("/query", h.HandleQuery)
		api.POST("/search", h.HandleSearch)
	}

	// 启动服务器
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	logger.Info(fmt.Sprintf("Server starting on %s", addr))
	logger.Info(fmt.Sprintf("Visit http://localhost:%s", cfg.Server.Port))

	if err := r.Run(addr); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
}
