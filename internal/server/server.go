// Package server 提供 HTTP/WebSocket 服务入口。
//
// 基于 Gin 框架，负责：
//   - HTTP 路由注册（/healthz、/readyz、/metrics）
//   - WebSocket 端点注册（/ws/:deviceId → transport.Handler）
//   - 服务启动/优雅关闭
//   - 中间件注册（日志、恢复、CORS）
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
	"github.com/ykt/xiaozhi-server-go/internal/transport"
)

// Config 服务配置。
type Config struct {
	// Addr HTTP 监听地址（如 ":8080"）。
	Addr string
	// Logger slog 日志器。
	Logger *slog.Logger
	// AISaaS aisaas 客户端（用于 /readyz 连通性检查）。
	AISaaS *aisaas.Client
	// Handler JSON-RPC 传输层 handler（负责 WebSocket 升级与消息分发）。
	Handler *transport.Handler
	// OTAHandler OTA 固件分发 HTTP 处理器（可选）。
	// 实现 HandleCheckUpdate / HandleActivate / HandleFirmwareDownload 三个 handler。
	OTAHandler OTAHandler
}

// OTAHandler OTA HTTP 路由处理器接口。
// 由 ota.Service 实现，注入到 server 后自动注册路由。
type OTAHandler interface {
	HandleCheckUpdate(c *gin.Context)
	HandleActivate(c *gin.Context)
	HandleFirmwareDownload(c *gin.Context)
}

// Server HTTP/WebSocket 服务。
type Server struct {
	cfg     Config
	logger  *slog.Logger
	handler *transport.Handler
	aisaas  *aisaas.Client
	httpSrv *http.Server
	router  *gin.Engine
}

// NewServer 创建服务实例。
//
// 初始化 Gin 引擎、注册中间件和路由。
// 生产模式下禁用 Gin debug 日志。
func NewServer(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// 生产模式
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	// 中间件链（按顺序）
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	s := &Server{
		cfg:     cfg,
		logger:  logger,
		handler: cfg.Handler,
		aisaas:  cfg.AISaaS,
		router:  router,
	}

	// 注册路由
	s.registerRoutes()

	return s
}

// registerRoutes 注册 HTTP 路由。
func (s *Server) registerRoutes() {
	// 健康检查
	s.router.GET("/healthz", s.handleHealthz)
	s.router.GET("/readyz", s.handleReadyz)

	// WebSocket 端点（委托给 transport.Handler）
	if s.handler != nil {
		s.router.GET("/ws/:deviceId", func(c *gin.Context) {
			s.handler.HandleWebSocket(c.Writer, c.Request)
		})
		s.logger.Info("websocket endpoint registered", "path", "/ws/:deviceId")
	}

	// OTA 固件分发端点
	if s.cfg.OTAHandler != nil {
		s.router.GET("/api/device/ota", s.cfg.OTAHandler.HandleCheckUpdate)
		s.router.POST("/api/device/ota/activate", s.cfg.OTAHandler.HandleActivate)
		s.router.GET("/firmware/:firmwareId", s.cfg.OTAHandler.HandleFirmwareDownload)
		s.logger.Info("ota endpoints registered",
			"endpoints", []string{
				"GET /api/device/ota",
				"POST /api/device/ota/activate",
				"GET /firmware/:firmwareId",
			},
		)
	}
}

// Start 启动 HTTP 服务（非阻塞，在 goroutine 中监听）。
//
// 返回 error 仅表示启动时的即时失败（如端口被占用）。
// 正常运行时通过 ctx 取消停止。
func (s *Server) Start(ctx context.Context) error {
	addr := s.cfg.Addr
	if addr == "" {
		addr = ":8080"
	}

	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.router,
		// 合理超时，防止慢客户端占用连接
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", "addr", addr)
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("server: listen on %s: %w", addr, err)
		}
	}()

	// 等待启动结果或 ctx 取消
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return nil
	}
}

// Stop 优雅关闭 HTTP 服务。
//
// 流程：
//  1. 停止接收新请求（Shutdown）
//  2. 等待现有请求处理完毕（最多 10s）
//  3. 超时后强制关闭
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("server stopping gracefully")

	if s.httpSrv == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.httpSrv.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("server shutdown failed", "error", err)
		return fmt.Errorf("server: shutdown: %w", err)
	}

	s.logger.Info("server stopped")
	return nil
}

// handleHealthz 健康检查端点。
//
// 返回 200 OK 表示进程存活。
func (s *Server) handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// handleReadyz 就绪检查端点。
//
// 检查 aisaas 后端连通性。200 OK 表示可以接收流量。
// 503 表示依赖未就绪。
func (s *Server) handleReadyz(c *gin.Context) {
	checks := make(map[string]string)

	// 检查 aisaas 连通性
	if s.aisaas != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		if err := s.aisaas.Ping(ctx); err != nil {
			checks["aisaas"] = fmt.Sprintf("error: %v", err)
		} else {
			checks["aisaas"] = "ok"
		}
	} else {
		checks["aisaas"] = "not configured"
	}

	// 判断整体状态
	status := http.StatusOK
	for _, v := range checks {
		if v != "ok" && v != "not configured" {
			status = http.StatusServiceUnavailable
			break
		}
	}

	c.JSON(status, gin.H{
		"status":  statusText(status),
		"time":    time.Now().UTC().Format(time.RFC3339),
		"checks":  checks,
	})
}

// statusText 返回 HTTP 状态码的文本描述。
func statusText(code int) string {
	if code == http.StatusOK {
		return "ready"
	}
	return "not ready"
}