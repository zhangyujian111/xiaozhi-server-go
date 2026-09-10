package aisaas

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Client ykt-aisaas 客户端 SDK。
//
// 封装 aisaas V2 全部接口调用，提供重试、限流、错误分类、请求追踪等能力。
// 所有接口调用均透传 request_id（UUID v7），并通过 slog 记录每次调用的延迟和状态码。
type Client struct {
	cfg        Config
	baseURL    string
	apiKey     string
	httpClient *http.Client
	limiter    *tokenBucketLimiter
	logger     *slog.Logger
	tracer     trace.Tracer
	retryCfg   RetryConfig
	deviceMgr  interface{ IncrementUsage() } // 可选，用于请求计数触发 API Key 轮换
}

// Config 客户端配置。
type Config struct {
	// BaseURL aisaas 服务地址，如 http://localhost:8190 或 https://ykt-aisaas:8443。
	BaseURL string
	// APIKey 设备 API Key（运行时注入，可后续通过 SetAPIKey 更新）。
	APIKey string
	// DeviceID 设备 ID。
	DeviceID string
	// InternalToken 内部接口鉴权 Token（X-Internal-Token）。
	InternalToken string
	// Logger slog 日志器（可选，nil 则使用默认）。
	Logger *slog.Logger
	// Timeout HTTP 请求超时（默认 30s）。
	Timeout time.Duration
	// MaxRetries 最大重试次数（默认 3）。
	MaxRetries int
	// RetryBackoff 重试退避基值（默认 1s）。
	RetryBackoff time.Duration
	// RateLimit 限流配置。Rate <= 0 表示不启用限流。
	RateLimit RateLimitConfig

	// ---- mTLS 配置（NICE v2，Phase 3 公网部署）----
	// 当 ClientCertFile + ClientKeyFile + CACertFile 三个字段均非空时，
	// HTTP Transport 将启用 mTLS：客户端出示证书，服务端证书由 CA 校验。
	// 仅在 BaseURL 为 https:// 时生效；http:// 时配置被忽略（开发模式）。

	// ClientCertFile 客户端证书文件路径（PEM）。
	ClientCertFile string
	// ClientKeyFile 客户端私钥文件路径（PEM）。
	ClientKeyFile string
	// CACertFile CA 证书文件路径（用于校验服务端证书）。
	CACertFile string
	// TLSServerName SNI / hostname 校验目标（默认 "ykt-aisaas"）。
	TLSServerName string
	// InsecureSkipVerify 跳过服务端证书校验（仅测试用，生产 MUST=false）。
	InsecureSkipVerify bool
}

// RateLimitConfig 限流配置。
type RateLimitConfig struct {
	// Rate 每秒允许的请求数。
	Rate float64
	// Burst 最大突发请求数。
	Burst int
}

// NewClient 创建 aisaas 客户端。
//
// 当 Config 中 ClientCertFile / ClientKeyFile / CACertFile 三个字段均非空时，
// 自动启用 mTLS HTTP Transport（最小 Go 版本 1.25+，依赖标准库）。
//
// 返回 error 的原因：
//   - 客户端证书 / 私钥加载失败（PEM 格式错误、文件不存在）
//   - CA 证书池加载失败
//
// 若 BaseURL 为 http://，mTLS 配置字段即便非空也会被忽略（HTTP 无 TLS）。
func NewClient(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 1 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.BaseURL != "" {
		cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	}

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	// mTLS 装载（仅在三个文件均配置且 BaseURL 为 https 时启用）
	if cfg.ClientCertFile != "" || cfg.ClientKeyFile != "" || cfg.CACertFile != "" {
		tlsCfg, err := buildClientTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("aisaas: build TLS config: %w", err)
		}
		transport.TLSClientConfig = tlsCfg
		cfg.Logger.Info("aisaas client mTLS enabled",
			"client_cert", cfg.ClientCertFile,
			"ca_cert", cfg.CACertFile,
			"server_name", cfg.TLSServerName,
		)
	}

	c := &Client{
		cfg:     cfg,
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		logger:   cfg.Logger,
		tracer:   otel.Tracer("aisaas-client"),
		retryCfg: DefaultRetryConfig,
	}

	if cfg.MaxRetries > 0 {
		c.retryCfg.MaxRetries = cfg.MaxRetries
	}
	if cfg.RetryBackoff > 0 {
		c.retryCfg.BaseBackoff = cfg.RetryBackoff
	}

	if cfg.RateLimit.Rate > 0 {
		burst := cfg.RateLimit.Burst
		if burst <= 0 {
			burst = int(cfg.RateLimit.Rate) + 1
		}
		c.limiter = newTokenBucketLimiter(cfg.RateLimit.Rate, burst)
	}

	return c, nil
}

// buildClientTLSConfig 构造客户端 mTLS *tls.Config。
func buildClientTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.ClientCertFile == "" || cfg.ClientKeyFile == "" {
		return nil, fmt.Errorf("client cert and key files are required (got cert=%q, key=%q)",
			cfg.ClientCertFile, cfg.ClientKeyFile)
	}
	if cfg.CACertFile == "" {
		return nil, fmt.Errorf("CA cert file is required for server verification")
	}

	cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load client keypair: %w", err)
	}

	caData, err := os.ReadFile(cfg.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caData) {
		return nil, fmt.Errorf("failed to parse CA cert PEM in %q", cfg.CACertFile)
	}

	serverName := cfg.TLSServerName
	if serverName == "" {
		serverName = "ykt-aisaas"
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
		ServerName:   serverName,
	}, nil
}

// SetAPIKey 运行时更新 API Key。
func (c *Client) SetAPIKey(apiKey string) {
	c.apiKey = apiKey
}

// SetBaseURL 运行时更新 BaseURL。
func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = strings.TrimRight(baseURL, "/")
}

// APIKey 返回当前 API Key。
func (c *Client) APIKey() string {
	return c.apiKey
}

// DeviceID 返回设备 ID。
func (c *Client) DeviceID() string {
	return c.cfg.DeviceID
}

// SetDeviceMgr 设置设备管理器（用于请求计数触发 API Key 轮换）。
func (c *Client) SetDeviceMgr(mgr interface{ IncrementUsage() }) {
	c.deviceMgr = mgr
}

// doRequest 通用 JSON HTTP 请求（带重试、限流、追踪、日志）。
//
// 返回：
//   - respBody: 响应体字节
//   - statusCode: HTTP 状态码
//   - error
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, authHeader string, result interface{}) ([]byte, int, error) {
	if c.limiter != nil && !c.limiter.Allow() {
		return nil, 0, ErrRateLimited
	}

	requestID := newRequestID()
	url := c.baseURL + path

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("aisaas: marshal request body: %w", err)
		}
	}

	spanName := fmt.Sprintf("aisaas.%s %s", strings.ToLower(method), path)
	ctx, span := c.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("http.method", method),
			attribute.String("http.url", url),
			attribute.String("aisaas.request_id", requestID),
		),
	)
	defer span.End()

	start := time.Now()
	var lastErr error

	for attempt := 0; attempt <= c.retryCfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := c.backoffDuration(attempt)
			c.logger.WarnContext(ctx, "aisaas retry",
				"attempt", attempt,
				"backoff_ms", backoff.Milliseconds(),
				"path", path,
				"request_id", requestID,
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				span.RecordError(ctx.Err())
				span.SetStatus(codes.Error, ctx.Err().Error())
				return nil, 0, ctx.Err()
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			lastErr = fmt.Errorf("aisaas: create request: %w", err)
			span.RecordError(lastErr)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", requestID)
		req.Header.Set("User-Agent", "xiaozhi-server-go/1.0 aisaas-sdk")

		c.setAuthHeader(req, authHeader)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("aisaas: request failed: %w", err)
			span.RecordError(lastErr)
			c.logger.ErrorContext(ctx, "aisaas request error",
				"error", err,
				"path", path,
				"request_id", requestID,
				"attempt", attempt,
			)
			if IsRetryable(lastErr) && attempt < c.retryCfg.MaxRetries {
				continue
			}
			return nil, 0, lastErr
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("aisaas: read response body: %w", readErr)
			span.RecordError(lastErr)
			return nil, 0, lastErr
		}

		latency := time.Since(start)
		span.SetAttributes(
			attribute.Int("http.status_code", resp.StatusCode),
			attribute.Int64("http.latency_ms", latency.Milliseconds()),
		)

		c.logger.InfoContext(ctx, "aisaas call",
			"method", method,
			"path", path,
			"status", resp.StatusCode,
			"latency_ms", latency.Milliseconds(),
			"request_id", requestID,
			"attempt", attempt,
		)

		// 429 限流
		if resp.StatusCode == 429 {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			c.logger.WarnContext(ctx, "aisaas rate limited",
				"path", path,
				"retry_after_ms", retryAfter.Milliseconds(),
				"request_id", requestID,
			)
			if attempt < c.retryCfg.MaxRetries {
				select {
				case <-time.After(retryAfter):
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				}
				continue
			}
			return respBody, resp.StatusCode, ErrRateLimited
		}

		// 401 / 402
		if resp.StatusCode == 401 || resp.StatusCode == 402 {
			lastErr = classifyError(resp.StatusCode, respBody)
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
			return respBody, resp.StatusCode, lastErr
		}

		// 5xx
		if resp.StatusCode >= 500 {
			lastErr = classifyError(resp.StatusCode, respBody)
			if attempt < c.retryCfg.MaxRetries {
				span.RecordError(lastErr)
				continue
			}
			span.SetStatus(codes.Error, lastErr.Error())
			return respBody, resp.StatusCode, lastErr
		}

		// 4xx 客户端错误
		if resp.StatusCode >= 400 {
			lastErr := classifyError(resp.StatusCode, respBody)
			span.RecordError(lastErr)
			span.SetStatus(codes.Error, lastErr.Error())
			return respBody, resp.StatusCode, lastErr
		}

		span.SetStatus(codes.Ok, "")
		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return respBody, resp.StatusCode, fmt.Errorf("aisaas: unmarshal response: %w", err)
			}
		}
		// 记录请求计数，触发 API Key 轮换
		if c.deviceMgr != nil {
			c.deviceMgr.IncrementUsage()
		}
		return respBody, resp.StatusCode, nil
	}

	span.SetStatus(codes.Error, "max retries exceeded")
	return nil, 0, fmt.Errorf("aisaas: max retries (%d) exceeded: %w", c.retryCfg.MaxRetries, lastErr)
}

// doRequestRaw 返回原始 http.Response（用于流式响应，不读取 body）。
func (c *Client) doRequestRaw(ctx context.Context, method, path string, body interface{}, authHeader string, extraHeaders map[string]string) (*http.Response, error) {
	if c.limiter != nil && !c.limiter.Allow() {
		return nil, ErrRateLimited
	}

	requestID := newRequestID()
	url := c.baseURL + path

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("aisaas: marshal request body: %w", err)
		}
	}

	spanName := fmt.Sprintf("aisaas.%s %s", strings.ToLower(method), path)
	ctx, span := c.tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("http.method", method),
			attribute.String("http.url", url),
			attribute.String("aisaas.request_id", requestID),
		),
	)
	defer span.End()

	start := time.Now()

	var reqBody io.Reader
	if bodyBytes != nil {
		reqBody = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("aisaas: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)
	req.Header.Set("User-Agent", "xiaozhi-server-go/1.0 aisaas-sdk")

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	c.setAuthHeader(req, authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("aisaas: request failed: %w", err)
	}

	latency := time.Since(start)
	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
		attribute.Int64("http.latency_ms", latency.Milliseconds()),
	)

	c.logger.InfoContext(ctx, "aisaas call",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"latency_ms", latency.Milliseconds(),
		"request_id", requestID,
	)

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		err := classifyError(resp.StatusCode, respBody)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// doRequestMultipart 发送 multipart/form-data 请求（用于 ASR）。
func (c *Client) doRequestMultipart(ctx context.Context, path string, fields map[string]string, fileField string, fileName string, fileData []byte, result interface{}) ([]byte, int, error) {
	if c.limiter != nil && !c.limiter.Allow() {
		return nil, 0, ErrRateLimited
	}

	requestID := newRequestID()
	url := c.baseURL + path

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for key, val := range fields {
		w.WriteField(key, val)
	}
	part, _ := w.CreateFormFile(fileField, fileName)
	part.Write(fileData)
	w.Close()
	contentType := w.FormDataContentType()

	ctx, span := c.tracer.Start(ctx, "aisaas.POST "+path,
		trace.WithAttributes(
			attribute.String("http.method", "POST"),
			attribute.String("http.url", url),
			attribute.String("aisaas.request_id", requestID),
		),
	)
	defer span.End()

	start := time.Now()

	for attempt := 0; attempt <= c.retryCfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := c.backoffDuration(attempt)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}

		bodyReader := bytes.NewReader(buf.Bytes())
		req, err := http.NewRequestWithContext(ctx, "POST", url, bodyReader)
		if err != nil {
			return nil, 0, fmt.Errorf("aisaas: create request: %w", err)
		}

		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-Request-ID", requestID)
		req.Header.Set("User-Agent", "xiaozhi-server-go/1.0 aisaas-sdk")
		c.setAuthHeader(req, "bearer")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			span.RecordError(err)
			if IsRetryable(err) && attempt < c.retryCfg.MaxRetries {
				continue
			}
			return nil, 0, err
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		latency := time.Since(start)
		span.SetAttributes(
			attribute.Int("http.status_code", resp.StatusCode),
			attribute.Int64("http.latency_ms", latency.Milliseconds()),
		)

		c.logger.InfoContext(ctx, "aisaas call",
			"method", "POST",
			"path", path,
			"status", resp.StatusCode,
			"latency_ms", latency.Milliseconds(),
			"request_id", requestID,
		)

		if resp.StatusCode >= 500 && attempt < c.retryCfg.MaxRetries {
			continue
		}

		if resp.StatusCode >= 400 {
			err := classifyError(resp.StatusCode, respBody)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return respBody, resp.StatusCode, err
		}

		span.SetStatus(codes.Ok, "")
		if result != nil && len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return respBody, resp.StatusCode, fmt.Errorf("aisaas: unmarshal response: %w", err)
			}
		}
		return respBody, resp.StatusCode, nil
	}

	return nil, 0, fmt.Errorf("aisaas: max retries exceeded")
}

// setAuthHeader 设置鉴权 Header。
func (c *Client) setAuthHeader(req *http.Request, authType string) {
	switch authType {
	case "bearer":
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
	case "internal":
		if c.cfg.InternalToken != "" {
			req.Header.Set("X-Internal-Token", c.cfg.InternalToken)
		}
	case "bearer+internal":
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		if c.cfg.InternalToken != "" {
			req.Header.Set("X-Internal-Token", c.cfg.InternalToken)
		}
	}
}

// backoffDuration 计算退避时间：1s, 2s, 4s, 8s, max 30s。
func (c *Client) backoffDuration(attempt int) time.Duration {
	backoff := time.Duration(1<<uint(attempt-1)) * c.retryCfg.BaseBackoff
	if backoff > c.retryCfg.MaxBackoff {
		backoff = c.retryCfg.MaxBackoff
	}
	return backoff
}

// parseRetryAfter 解析 Retry-After header。
func parseRetryAfter(val string) time.Duration {
	if val == "" {
		return 1 * time.Second
	}
	if d, err := time.ParseDuration(val + "s"); err == nil {
		return d
	}
	return 1 * time.Second
}
