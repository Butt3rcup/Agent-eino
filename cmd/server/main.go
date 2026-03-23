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
	"go-eino-agent/internal/bootstrap"
	"go-eino-agent/internal/handler"
	"go-eino-agent/pkg/logger"
)

func main() {
	if err := logger.Init(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		logger.Fatal("Invalid config", zap.Error(err))
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	deps, err := bootstrap.Build(appCtx, cfg)
	if err != nil {
		logger.Fatal("Failed to bootstrap application", zap.Error(err))
	}

	h, err := handler.NewHandler(deps)
	if err != nil {
		logger.Fatal("Failed to create handler", zap.Error(err))
	}
	defer h.Close()

	r := gin.New()
	r.Use(gin.Recovery())
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
	r.Static("/static", "./web/static")
	r.LoadHTMLGlob("web/templates/*")
	r.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.html", gin.H{"title": "网络热词 RAG 系统"})
	})

	api := r.Group("/api")
	{
		api.GET("/health", h.HandleHealth)
		api.GET("/metrics", metrics.handleMetrics)
		api.GET("/upload/:taskID", h.HandleUploadStatus)
		api.POST("/upload", h.HandleUpload)
		api.POST("/query", h.HandleQuery)
		api.POST("/search", h.HandleSearch)
	}

	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	logger.Info(fmt.Sprintf("Server starting on %s", addr))
	logger.Info(fmt.Sprintf("Visit http://localhost:%s", cfg.Server.Port))
	logger.Info("Runtime config",
		zap.Float64("rate_limit_rps", cfg.Security.RateLimitRPS),
		zap.Float64("rate_limit_burst", cfg.Security.RateLimitBurst),
		zap.Int("query_default_timeout_sec", cfg.Security.QueryDefaultTimeoutSec),
		zap.Float64("query_default_rate_limit_rps", cfg.Security.QueryDefaultRateLimitRPS),
		zap.Float64("query_default_rate_limit_burst", cfg.Security.QueryDefaultRateLimitBurst),
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
	appCancel()
	logger.Info("Server stopped gracefully")
}
