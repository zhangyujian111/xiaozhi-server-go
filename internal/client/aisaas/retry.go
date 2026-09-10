package aisaas

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ============================================================
// 哨兵错误
// ============================================================

var (
	// ErrAuthFailed 鉴权失败（401）。
	ErrAuthFailed = errors.New("aisaas: authentication failed")
	// ErrKeyExpired API Key 已过期（401 + key expired）。
	ErrKeyExpired = errors.New("aisaas: api key expired")
	// ErrQuotaExceeded 配额不足（402）。
	ErrQuotaExceeded = errors.New("aisaas: quota exceeded")
	// ErrRateLimited 请求过于频繁（429）。
	ErrRateLimited = errors.New("aisaas: rate limited")
	// ErrServerError 服务端错误（5xx）。
	ErrServerError = errors.New("aisaas: server error")
	// ErrNetworkError 网络错误（超时/断连）。
	ErrNetworkError = errors.New("aisaas: network error")
	// ErrNotFound 资源不存在（404）。
	ErrNotFound = errors.New("aisaas: not found")
	// ErrForbidden 权限不足（403）。
	ErrForbidden = errors.New("aisaas: forbidden")
	// ErrConflict 资源冲突（409）。
	ErrConflict = errors.New("aisaas: conflict")
	// ErrBadRequest 参数错误（400）。
	ErrBadRequest = errors.New("aisaas: bad request")
)

// ============================================================
// 错误分类
// ============================================================

// IsRetryable 判断错误是否可重试。
// 网络错误、超时、5xx 服务端错误可重试；4xx 客户端错误不可重试。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 哨兵错误
	if errors.Is(err, ErrServerError) || errors.Is(err, ErrRateLimited) {
		return true
	}
	if errors.Is(err, ErrNetworkError) {
		return true
	}

	// 网络错误
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	// URL 错误
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return IsRetryable(urlErr.Err)
	}

	// 系统错误
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	return false
}

// IsAuthFailure 判断是否鉴权失败（401）。
func IsAuthFailure(err error) bool {
	return errors.Is(err, ErrAuthFailed) || errors.Is(err, ErrKeyExpired)
}

// IsQuotaExceeded 判断是否配额不足（402）。
func IsQuotaExceeded(err error) bool {
	return errors.Is(err, ErrQuotaExceeded)
}

// classifyError 根据 HTTP 状态码和响应体分类错误。
func classifyError(statusCode int, body []byte) error {
	switch statusCode {
	case 400:
		return ErrBadRequest
	case 401:
		if isKeyExpiredResponse(body) {
			return ErrKeyExpired
		}
		return ErrAuthFailed
	case 402:
		return ErrQuotaExceeded
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 409:
		return ErrConflict
	case 429:
		return ErrRateLimited
	default:
		if statusCode >= 500 {
			return fmt.Errorf("%w: status %d", ErrServerError, statusCode)
		}
		return fmt.Errorf("aisaas: unexpected status %d", statusCode)
	}
}

// isKeyExpiredResponse 检测响应体是否包含 key expired 信息。
func isKeyExpiredResponse(body []byte) bool {
	bodyStr := string(body)
	return strings.Contains(bodyStr, "expired") ||
		strings.Contains(bodyStr, "key_expired") ||
		strings.Contains(bodyStr, "invalid_api_key")
}

// ============================================================
// 重试配置
// ============================================================

// RetryConfig 重试配置。
type RetryConfig struct {
	// MaxRetries 最大重试次数（默认 3）。
	MaxRetries int
	// BaseBackoff 退避基值（默认 1s）。
	BaseBackoff time.Duration
	// MaxBackoff 最大退避时间（默认 30s）。
	MaxBackoff time.Duration
	// RetryOnCodes 触发重试的 HTTP 状态码。
	RetryOnCodes []int
}

// DefaultRetryConfig 默认重试配置。
var DefaultRetryConfig = RetryConfig{
	MaxRetries:   3,
	BaseBackoff:  1 * time.Second,
	MaxBackoff:   30 * time.Second,
	RetryOnCodes: []int{429, 500, 502, 503, 504},
}

// ============================================================
// 令牌桶限流器
// ============================================================

// tokenBucketLimiter 本地令牌桶限流器。
type tokenBucketLimiter struct {
	mu       sync.Mutex
	tokens   float64
	lastTime time.Time
	rate     float64 // 每秒令牌数
	burst    float64 // 最大突发
}

// newTokenBucketLimiter 创建令牌桶限流器。
func newTokenBucketLimiter(rate float64, burst int) *tokenBucketLimiter {
	if burst <= 0 {
		burst = int(math.Ceil(rate))
	}
	return &tokenBucketLimiter{
		tokens:   float64(burst),
		lastTime: time.Now(),
		rate:     rate,
		burst:    float64(burst),
	}
}

// Allow 检查是否允许请求。
func (r *tokenBucketLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastTime).Seconds()
	r.tokens += elapsed * r.rate
	if r.tokens > r.burst {
		r.tokens = r.burst
	}
	r.lastTime = now

	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

// ============================================================
// UUID v7 生成
// ============================================================

// newRequestID 生成 UUID v7 格式的 request_id。
func newRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// 降级为 UUID v4
		return uuid.New().String()
	}
	return id.String()
}

// ============================================================
// API 响应解析辅助
// ============================================================

// parseAPIResponse 解析 SaaS 自有接口统一信封。
func parseAPIResponse(body []byte, result interface{}) error {
	var env APIResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("aisaas: parse response envelope: %w", err)
	}
	if env.Code != 0 && env.Code != 200 {
		return fmt.Errorf("aisaas: api error code=%d message=%s", env.Code, env.Message)
	}
	// 如果 env.Data 不为 nil，则提取 data 到 result
	if env.Data != nil && result != nil {
		dataBytes, err := json.Marshal(env.Data)
		if err != nil {
			return fmt.Errorf("aisaas: marshal data: %w", err)
		}
		if err := json.Unmarshal(dataBytes, result); err != nil {
			return fmt.Errorf("aisaas: unmarshal data: %w", err)
		}
	}
	return nil
}

// parseAPIError 解析 OpenAI 兼容格式错误。
func parseAPIError(body []byte) error {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return fmt.Errorf("aisaas: parse error: %w", err)
	}
	return fmt.Errorf("aisaas: %s (type=%s code=%s)",
		apiErr.Error.Message, apiErr.Error.Type, apiErr.Error.Code)
}
