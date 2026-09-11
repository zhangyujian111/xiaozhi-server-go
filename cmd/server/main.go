// Package main 是 xiaozhi-server-go 的启动入口。
//
// 启动流程（14 阶段）：
//  1. 加载配置（viper）
//  2. 初始化日志（zerolog + slog）
//  3. 信号处理（SIGINT/SIGTERM）
//  4. Prometheus metrics（可选）
//  5. OpenTelemetry tracer（可选）
//  6. 设备 API Key 管理（device.NewManager + Start）
//  7. aisaas 客户端（连通性验证 + API Key 注入）
//  8. IoT 设备管理器（内存 mock）
//  9. OTA 固件分发服务（HTTP 端点 + 内存元数据）
//  10. session/persona 预加载（可选缓存）
//  11. P1 业务模块（music / file / template / asr / tts / mcp）
//  12. WebSocket server（注册 JSON-RPC handler + 回调 + OTA 端点）
//  13. 后台 worker（device.RotateKeyWorker）
//  14. 优雅关闭（10s timeout）
//
// 协议栈：
//
//	ESP32 ←→ WebSocket ←→ xiaozhi-server-go ←→ HTTP ←→ ykt-aisaas
//	  (Opus)     ws://        (Go)             REST      (AI 底座)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ykt/xiaozhi-server-go/internal/admin"
	"github.com/ykt/xiaozhi-server-go/internal/asr"
	"github.com/ykt/xiaozhi-server-go/internal/audio"
	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
	"github.com/ykt/xiaozhi-server-go/internal/config"
	"github.com/ykt/xiaozhi-server-go/internal/configmgr"
	"github.com/ykt/xiaozhi-server-go/internal/device"
	"github.com/ykt/xiaozhi-server-go/internal/file"
	"github.com/ykt/xiaozhi-server-go/internal/iot"
	"github.com/ykt/xiaozhi-server-go/internal/mcp"
	"github.com/ykt/xiaozhi-server-go/internal/music"
	"github.com/ykt/xiaozhi-server-go/internal/observability"
	"github.com/ykt/xiaozhi-server-go/internal/ota"
	"github.com/ykt/xiaozhi-server-go/internal/redisclient"
	"github.com/ykt/xiaozhi-server-go/internal/server"
	"github.com/ykt/xiaozhi-server-go/internal/session"
	"github.com/ykt/xiaozhi-server-go/internal/template"
	"github.com/ykt/xiaozhi-server-go/internal/transport"
	"github.com/ykt/xiaozhi-server-go/internal/tts"
	"github.com/ykt/xiaozhi-server-go/internal/user"
	"github.com/ykt/xiaozhi-server-go/internal/vision"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	// =========================================================================
	// 阶段 1: 加载配置（viper）
	// =========================================================================
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config validation failed: %v\n", err)
		os.Exit(1)
	}

	// =========================================================================
	// 阶段 2: 初始化日志（zerolog + slog）
	// =========================================================================
	zl := observability.NewLogger(cfg.Log)
	slog.SetDefault(slog.New(observability.NewSlogHandler(zl)))
	logger := slog.Default()

	logger.Info("phase 2/11: logger initialized",
		"level", cfg.Log.Level,
		"format", cfg.Log.Format,
		"output", cfg.Log.Output,
	)

	// =========================================================================
	// 阶段 3: 信号处理（SIGINT/SIGTERM）
	// =========================================================================
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("phase 3/11: signal handler registered",
		"signals", []string{"SIGINT", "SIGTERM"},
	)

	// =========================================================================
	// 阶段 4: Prometheus metrics（可选）
	// =========================================================================
	var tracerShutdown func(context.Context) error
	var metricsSrv *http.Server
	var metrics *observability.Metrics

	if cfg.Observability.Metrics.Enabled {
		// 创建并注册 metrics
		metrics = observability.NewMetrics(cfg.Observability.Metrics)
		metrics.MustRegister()
		metricsAddr := fmt.Sprintf(":%d", cfg.Observability.Metrics.Port)
		metricsPath := cfg.Observability.Metrics.Path
		if metricsPath == "" {
			metricsPath = "/metrics"
		}

		mux := http.NewServeMux()
		mux.Handle(metricsPath, promhttp.Handler())

		metricsSrv = &http.Server{
			Addr:    metricsAddr,
			Handler: mux,
		}

		go func() {
			logger.Info("phase 4/11: prometheus metrics started",
				"addr", metricsAddr,
				"path", metricsPath,
			)
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("metrics server failed", "error", err)
			}
		}()
	} else {
		logger.Info("phase 4/11: prometheus metrics disabled")
	}

	// =========================================================================
	// 阶段 5: OpenTelemetry tracer（可选）
	// =========================================================================
	if cfg.Observability.Tracing.Enabled {
		shutdown, err := observability.InitTracer(ctx, cfg.Observability.Tracing, cfg.Observability.ServiceName)
		if err != nil {
			logger.Error("tracer init failed, continuing without tracing", "error", err)
		} else {
			tracerShutdown = shutdown
			logger.Info("phase 5/11: opentelemetry tracer initialized",
				"exporter", cfg.Observability.Tracing.Exporter,
				"endpoint", cfg.Observability.Tracing.Endpoint,
				"sample_rate", cfg.Observability.Tracing.SampleRate,
			)
		}
	} else {
		logger.Info("phase 5/11: opentelemetry tracer disabled")
	}

	// =========================================================================
	// 阶段 5b: OTel metrics（V4-O）
	// =========================================================================
	var otelMetricsShutdown func(context.Context) error
	if cfg.Observability.OTel.MetricsEnabled {
		shutdown, err := observability.InitOTelMetrics(observability.OTelMetricsConfig{
			ExporterURL: cfg.Observability.OTel.ExporterURL,
			ServiceName: cfg.Observability.ServiceName,
			Interval:    time.Duration(cfg.Observability.OTel.ExportInterval) * time.Second,
		})
		if err != nil {
			logger.Error("otel metrics init failed, continuing without otel metrics", "error", err)
		} else {
			otelMetricsShutdown = shutdown
			logger.Info("phase 5b/11: otel metrics initialized",
				"exporter_url", cfg.Observability.OTel.ExporterURL,
				"interval", cfg.Observability.OTel.ExportInterval,
			)
		}
	} else {
		logger.Info("phase 5b/11: otel metrics disabled")
	}

	// =========================================================================
	// 阶段 5c: OTel logs（V4-O）
	// =========================================================================
	var otelLogsShutdown func(context.Context) error
	if cfg.Observability.OTel.LogsEnabled {
		shutdown, err := observability.InitLogs(observability.LogsConfig{
			ExporterURL: cfg.Observability.OTel.ExporterURL,
			ServiceName: cfg.Observability.ServiceName,
		})
		if err != nil {
			logger.Error("otel logs init failed, continuing without otel logs", "error", err)
		} else {
			otelLogsShutdown = shutdown
			logger.Info("phase 5c/11: otel logs initialized",
				"exporter_url", cfg.Observability.OTel.ExporterURL,
			)
		}
	} else {
		logger.Info("phase 5c/11: otel logs disabled")
	}

	// =========================================================================
	// 阶段 6: 设备 API Key 管理（device.NewManager + Start）
	// =========================================================================

	// 先创建 aisaas 客户端（用于设备注册/验证）
	aisaasClient, err := aisaas.NewClient(aisaas.Config{
		BaseURL:        cfg.Aisaas.EffectiveURL(),
		InternalToken:  cfg.Aisaas.InternalToken,
		Logger:         logger,
		Timeout:        cfg.Aisaas.Timeout,
		MaxRetries:     cfg.Aisaas.MaxRetries,
		RetryBackoff:   cfg.Aisaas.RetryBackoff,
		ClientCertFile: cfg.Aisaas.TLS.ClientCertFile,
		ClientKeyFile:  cfg.Aisaas.TLS.ClientKeyFile,
		CACertFile:     cfg.Aisaas.TLS.CACertFile,
		TLSServerName:  cfg.Aisaas.TLS.ServerName,
	})
	if err != nil {
		logger.Error("create aisaas client", "error", err)
		os.Exit(1)
	}

	// 后续会注入 deviceMgr 用于请求计数触发轮换

	// 验证 aisaas 连通性
	pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
	if err := aisaasClient.Ping(pingCtx); err != nil {
		logger.Error("aisaas unreachable, check network and configuration",
			"base_url", cfg.Aisaas.EffectiveURL(),
			"error", err,
		)
		pingCancel()
		os.Exit(1)
	}
	pingCancel()
	logger.Info("aisaas connectivity verified", "base_url", cfg.Aisaas.EffectiveURL())

	// 创建设备管理器
	devCfg := &device.Config{
		EncryptedKeyFile: cfg.Device.EncryptedKeyFile,
		EFUSEMAC:         cfg.Device.MACID,
		DeviceID:         cfg.Device.MACID, // 使用 MAC 作为初始 deviceID
		Firmware:         cfg.Observability.ServiceVersion,
	}

	devMgr, err := device.NewManager(devCfg, aisaasClient, logger)
	if err != nil {
		logger.Error("device manager init failed", "error", err)
		os.Exit(1)
	}

	// 执行设备鉴权（加载或注册 API Key）— dev 模式失败时跳过
	if err := devMgr.Start(ctx); err != nil {
		logger.Warn("device authentication failed (dev mode, continuing)", "error", err)
	}
	logger.Info("phase 6/11: device manager started",
		"registered", devMgr.IsRegistered(),
		"key_id", devMgr.GetKeyID(),
	)

	// 注入 deviceMgr 到 aisaasClient，用于请求计数触发 API Key 轮换
	aisaasClient.SetDeviceMgr(devMgr)

	// =========================================================================
	// 阶段 7: aisaas 客户端（API Key 已由 Manager.Start 注入）
	// =========================================================================
	// API Key 已在 devMgr.Start() 中通过 client.SetAPIKey() 注入
	logger.Info("phase 7/11: aisaas client ready",
		"base_url", cfg.Aisaas.EffectiveURL(),
		"has_api_key", aisaasClient.APIKey() != "",
	)

	// =========================================================================
	// 阶段 8: IoT 设备管理器（内存 mock）
	// =========================================================================
	iotMgr := iot.NewManager(logger)
	logger.Info("phase 8/13: iot device manager initialized",
		"mode", "memory-mock",
		"delay_ms", 50,
	)

// =========================================================================
// 阶段 9: OTA 固件分发服务（HTTP 端点 + 内存元数据）
// =========================================================================

	// 9a: Redis 客户端（设备激活注册表 backend）
	rdb, err := redisclient.New(ctx, redisclient.Config{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}, logger)
	if err != nil {
		logger.Error("redis init failed (required for device activation)",
			"addr", cfg.Redis.Addr, "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	deviceRegistry := ota.NewDeviceRegistry(rdb)
	logger.Info("phase 9a/13: device registry (Redis) initialized",
		"code_ttl", "5m",
		"pending_set", "ota:devices:pending",
		"activated_set", "ota:devices:activated",
	)

	serverURL := fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	otaSvc := ota.NewService(logger, serverURL, deviceRegistry)
	logger.Info("phase 9/13: ota firmware service initialized",
		"server_url", serverURL,
		"endpoints", []string{
			"GET /api/device/ota",
			"POST /api/device/ota/activate",
			"GET /firmware/:firmwareId",
		},
	)

	// =========================================================================
	// 阶段 10: P0 业务模块装配（session / user / device-meta / configmgr）
	// =========================================================================

	// 10a: session 管理器（内存存储 + 定时清理 worker）
	sessionMgr := session.NewManager(logger)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				logger.Info("session cleanup worker stopped")
				return
			case <-ticker.C:
				cleaned, _ := sessionMgr.Cleanup(ctx, 24*time.Hour)
				if cleaned > 0 {
					logger.Info("session cleanup completed", "cleaned", cleaned)
				}
			}
		}
	}()
	logger.Info("phase 10a/14: session manager started",
		"cleanup_interval", "5m",
		"idle_timeout", "24h",
	)

	// 10b: user 管理器（内存 mock + BCrypt + 5 测试用户）
	userMgr := user.NewManager(logger)
	logger.Info("phase 10b/14: user manager started",
		"mode", "memory-mock",
		"seed_users", 5,
	)

	// 10c: device 元数据管理器（扩展 device 包，CRUD + 人设绑定）
	deviceMetaMgr := device.NewDeviceManager(logger)
	logger.Info("phase 10c/14: device metadata manager started",
		"mode", "memory",
	)

	// 10d: configmgr 管理器（内存存储 + viper 默认配置 fallback）
	configMgr := configmgr.NewManager(logger)
	logger.Info("phase 10d/14: config manager started",
		"default_configs", 13,
	)

	// 抑制未使用变量警告（P0 模块后续步骤将注入到 handler/路由）
	_, _, _, _ = sessionMgr, userMgr, deviceMetaMgr, configMgr

	// =========================================================================
	// 阶段 11: P1 业务模块（music / file / template / asr / tts / mcp）
	// =========================================================================

	// music: 内存 mock 5 首测试曲目，播放状态按设备维度管理
	musicSvc := music.NewService()

	// file: 本地文件系统存储，上传目录 ./tmp/uploads/，0600 权限
	fileSvc := file.NewService()

	// template: 内存 mock 5 个 Go text/template，支持渲染与变量提取
	templateMgr := template.NewManager()

	// asr: 委托 aisaas SDK 调用语音识别
	asrSvc := asr.NewService(aisaasClient)

	// tts: 委托 aisaas SDK 调用语音合成
	ttsSvc := tts.NewService(aisaasClient)

	// mcp: 本地管理服务器注册 + 委托 aisaas SDK 调用工具
	mcpClient := mcp.NewClient(aisaasClient)

	// 抑制未使用变量警告（P1 模块后续步骤将注入到 handler/路由）
	_, _, _, _, _, _ = musicSvc, fileSvc, templateMgr, asrSvc, ttsSvc, mcpClient

	logger.Info("phase 11/14: p1 business modules initialized",
		"modules", []string{"music", "file", "template", "asr", "tts", "mcp"},
	)

	// =========================================================================
	// 阶段 11b: 音频处理器（Opus 编解码 + VAD 语音检测 + AEC 回声消除）
	// =========================================================================
	audioPipeline, err := audio.NewFullPipelineWithLogger(logger)
	if err != nil {
		logger.Error("audio pipeline init failed (check libopus system dependency)",
			"error", err,
			"hint_centos", "yum install -y opus-devel",
			"hint_debian", "apt install -y libopus-dev",
		)
		os.Exit(1)
	}
	defer audioPipeline.Close()

	logger.Info("phase 11b/14: audio pipeline initialized",
		"sample_rate", audioPipeline.SampleRate(),
		"frame_size", audioPipeline.FrameSize(),
		"codec", "opus",
		"vad", "energy-rms",
		"aec", audioPipeline.IsAECEnabled(),
		"components", []string{"AEC", "NS", "AGC", "Opus", "VAD"},
	)

	// =========================================================================
	// 阶段 11c: 视觉模块（per-connection pipeline 装配）
	// =========================================================================
	// 视觉流水线不在全局启动，而是 per-connection 装配：
	//   - 每个 WebSocket 连接在 HandleWebSocket 内创建独立的
	//     vision.FrameReceiver + vision.FaceFollower
	//   - 输出通道 (servoCh) 通过 drain goroutine 写入 wsConn.SendServo
	// v1 范围：仅 detect + 5 点 keypoints + 跟踪 + servo；无 face_profile 下行。
	// 这里只准备配置，注入到 transport.Handler 即可。
	var visionCfg *vision.VisionConfig
	visionImageW, visionImageH := 640, 480 // 默认图像尺寸；后续可从 device capability 动态获取
	if cfg.Vision.Enabled {
		visionCfg = &vision.VisionConfig{
			Enabled:      cfg.Vision.Enabled,
			CameraFPS:    cfg.Vision.CameraFPS,
			FollowGapMs:  cfg.Vision.FollowGapMs,
			DeadZonePx:   cfg.Vision.DeadZonePx,
			HFovDeg:      cfg.Vision.HFovDeg,
			MinPulseUs:   cfg.Vision.MinPulseUs,
			MaxPulseUs:   cfg.Vision.MaxPulseUs,
			CenterPanUs:  cfg.Vision.CenterPanUs,
			CenterTiltUs: cfg.Vision.CenterTiltUs,
			RangePanDeg:  cfg.Vision.RangePanDeg,
			RangeTiltDeg: cfg.Vision.RangeTiltDeg,
			InvertPan:    cfg.Vision.InvertPan,
			InvertTilt:   cfg.Vision.InvertTilt,
		}
		logger.Info("phase 11c/14: vision enabled (per-conn pipeline)",
			"camera_fps", cfg.Vision.CameraFPS,
			"follow_gap_ms", cfg.Vision.FollowGapMs,
			"dead_zone_px", cfg.Vision.DeadZonePx,
			"image_size", fmt.Sprintf("%dx%d", visionImageW, visionImageH),
		)
	} else {
		logger.Info("phase 11c/14: vision disabled")
	}

	// =========================================================================
	// 阶段 12: WebSocket server（注册 JSON-RPC handler + 回调 + OTA 端点）
	// =========================================================================

	// 创建 JSON-RPC 传输层 handler
	wsHandler := transport.NewHandler(nil, cfg.WebSocket)

	// 注入 vision 配置（per-conn pipeline 装配用）
	wsHandler.SetVisionConfig(aisaasClient, visionCfg, logger, visionImageW, visionImageH)

	// 注册业务回调（7 个 OnXxx 回调）
	wsHandler.OnHello = server.NewOnHello(aisaasClient, logger)
	wsHandler.OnListen = server.NewOnListen(aisaasClient, audioPipeline, logger)
	wsHandler.OnAbort = server.NewOnAbort(logger)
	wsHandler.OnMCP = server.NewOnMCP(aisaasClient, logger)
	wsHandler.OnIot = server.NewOnIoT(iotMgr, logger)
	wsHandler.OnCamera = server.NewOnCamera(aisaasClient, logger)
	wsHandler.OnCameraVideo = server.NewOnCameraVideo(aisaasClient, logger)

	// Vision 扩展：注册 servo ACK 回调（vision-servo v2）
	if cfg.Vision.Enabled {
		wsHandler.OnServoAck = server.NewOnServoAck(logger)
	}

	logger.Info("phase 12/14: json-rpc callbacks registered",
		"callbacks", []string{"OnHello", "OnListen", "OnAbort", "OnMCP", "OnIot", "OnCamera", "OnCameraVideo"},
	)

	// 创建 HTTP/WebSocket 服务
	// =========================================================================
	// 阶段 11d: Admin 模块（JWT 鉴权 + 设备激活管理）
	// =========================================================================
	adminCfg := admin.Config{
		Username:     cfg.Admin.Username,
		PasswordHash: cfg.Admin.PasswordHash,
		JWTSecret:    cfg.Admin.JWTSecret,
		TokenTTL:     cfg.Admin.TokenTTL,
	}
	if adminCfg.PasswordHash == "" && cfg.Admin.Password != "" {
		// 启动时若配置了明文密码，自动 bcrypt 后填入 hash
		h, err := bcrypt.GenerateFromPassword([]byte(cfg.Admin.Password), bcrypt.DefaultCost)
		if err != nil {
			logger.Error("admin password bcrypt failed", "error", err)
			os.Exit(1)
		}
		adminCfg.PasswordHash = string(h)
		logger.Info("admin password auto-hashed from plaintext config")
	}
	if adminCfg.JWTSecret == "" || len(adminCfg.JWTSecret) < 32 {
		logger.Error("admin.jwt_secret must be set and ≥ 32 bytes",
			"len", len(adminCfg.JWTSecret))
		os.Exit(1)
	}
	adminModule := admin.NewModule(adminCfg, deviceRegistry, logger)
	logger.Info("phase 11d/14: admin module initialized",
		"username", adminCfg.Username,
		"endpoints", []string{
			"POST /api/admin/auth/login",
			"GET  /api/admin/auth/me",
			"POST /api/admin/devices/activate-by-code",
			"GET  /api/admin/devices/pending",
			"GET  /api/admin/devices/activated",
		},
	)

	wsServer := server.NewServer(server.Config{
		Addr:       cfg.Server.EffectiveAddr(),
		Logger:     logger,
		AISaaS:     aisaasClient,
		Handler:    wsHandler,
		OTAHandler: otaSvc,
		Admin:      adminModule,
	})

	// 启动服务（非阻塞）
	go func() {
		if err := wsServer.Start(ctx); err != nil {
			logger.Error("http/ws server failed", "error", err)
			// 触发优雅关闭
			cancel()
		}
	}()

	// =========================================================================
	// 阶段 13: 后台 worker（device.RotateKeyWorker）
	// =========================================================================
	go devMgr.RotateKeyWorker(ctx)
	logger.Info("phase 13/14: background workers started",
		"workers", []string{"RotateKeyWorker"},
	)

	// =========================================================================
	// 阶段 14: 优雅关闭（10s timeout）
	// =========================================================================
	logger.Info("phase 14/14: xiaozhi-server-go started",
		"addr", cfg.Server.EffectiveAddr(),
		"version", cfg.Observability.ServiceVersion,
		"service", cfg.Observability.ServiceName,
	)

	// 等待退出信号
	<-ctx.Done()
	logger.Info("shutdown signal received, gracefully shutting down")

	// 创建关闭超时上下文
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// 关闭 HTTP/WebSocket 服务
	if err := wsServer.Stop(shutdownCtx); err != nil {
		logger.Error("http/ws server stop failed", "error", err)
	}

	// 关闭 Prometheus metrics 服务
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			logger.Error("metrics server stop failed", "error", err)
		}
	}

	// vision 模块关闭（per-conn pipeline 随 ctx 取消自动退出，此处仅日志）
	if cfg.Vision.Enabled {
		logger.Info("vision modules closed (per-conn pipelines drained)")
	}

	// 关闭 OpenTelemetry tracer
	if tracerShutdown != nil {
		if err := tracerShutdown(shutdownCtx); err != nil {
			logger.Error("tracer shutdown failed", "error", err)
		}
	}

	// 关闭 OTel metrics
	if otelMetricsShutdown != nil {
		if err := otelMetricsShutdown(shutdownCtx); err != nil {
			logger.Error("otel metrics shutdown failed", "error", err)
		}
	}

	// 关闭 OTel logs
	if otelLogsShutdown != nil {
		if err := otelLogsShutdown(shutdownCtx); err != nil {
			logger.Error("otel logs shutdown failed", "error", err)
		}
	}

	// vision 模块关闭（per-conn pipeline 随 ctx 取消自动退出，此处仅日志）
	if cfg.Vision.Enabled {
		logger.Info("vision modules closed (per-conn pipelines drained)")
	}

	logger.Info("xiaozhi-server-go stopped gracefully")
}
