package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid config", zap.Error(err))
	}

	// 创建 Handler
	h, err := handler.NewHandler(cfg)
	if err != nil {
		logger.Fatal("Failed to create handler", zap.Error(err))
	}
	defer h.Close()

	// 创建 Gin 路由
	r := gin.Default()
	metrics := newRequestMetrics()
	limiter := newRateLimiter(cfg.Security.RateLimitRPS, cfg.Security.RateLimitBurst)

	var trustedProxies []string
	if len(cfg.Server.TrustedProxies) > 0 {
		trustedProxies = cfg.Server.TrustedProxies
	}
	if err := r.SetTrustedProxies(trustedProxies); err != nil {
		logger.Fatal("Invalid TRUSTED_PROXIES", zap.Error(err))
	}

	r.Use(buildCORSMiddleware(cfg))
	r.Use(limiter.middleware())
	r.Use(metrics.middleware())

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
		api.GET("/metrics", metrics.handleMetrics)
		api.POST("/upload", h.HandleUpload)
		api.POST("/query", h.HandleQuery)
		api.POST("/search", h.HandleSearch)
	}

	// 启动服务器
	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	logger.Info(fmt.Sprintf("Server starting on %s", addr))
	logger.Info(fmt.Sprintf("Visit http://localhost:%s", cfg.Server.Port))
	logger.Info("Runtime config",
		zap.Float64("rate_limit_rps", cfg.Security.RateLimitRPS),
		zap.Float64("rate_limit_burst", cfg.Security.RateLimitBurst),
		zap.Int("read_timeout_sec", cfg.Server.ReadTimeoutSec),
		zap.Int("write_timeout_sec", cfg.Server.WriteTimeoutSec),
		zap.Int("shutdown_timeout_sec", cfg.Server.ShutdownTimeoutSec),
	)

	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("Shutdown signal received")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeoutSec)*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Graceful shutdown failed", zap.Error(err))
		return
	}
	logger.Info("Server stopped gracefully")
}
