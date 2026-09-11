// Package admin 提供 xiaozhi-admin web 端的后台管理 API（JWT 鉴权 + 设备激活管理）。
//
// 设计要点：
//   - 单一 admin 账号（username/password 从配置读取，bcrypt 校验）
//   - JWT 签名密钥从 xiaozhi-server-go 主配置中的 admin.jwt_secret 读取
//   - 设备激活管理复用 ota.DeviceRegistry
//   - 所有 admin endpoints 走 JWT 中间件（除 /api/admin/auth/login）
package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ykt/xiaozhi-server-go/internal/ota"
	"golang.org/x/crypto/bcrypt"
)

// Config admin 模块配置（与 xiaozhi-server-go config.Config.Admin 对齐）。
type Config struct {
	Username     string // 单管理员账号
	PasswordHash string // bcrypt 哈希后的密码
	JWTSecret    string // JWT 签名密钥（≥ 32 字节）
	TokenTTL     time.Duration
}

// DefaultConfig 默认值。
func DefaultConfig() Config {
	return Config{
		Username:     "admin",
		PasswordHash: "", // 必须外部设置
		JWTSecret:    "", // 必须外部设置
		TokenTTL:     24 * time.Hour,
	}
}

// Claims JWT 自定义 claims。
type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Module admin 模块（注入到 server.go 路由）。
type Module struct {
	cfg      Config
	logger   *slog.Logger
	registry *ota.DeviceRegistry
}

// NewModule 创建 admin 模块。
func NewModule(cfg Config, registry *ota.DeviceRegistry, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	return &Module{cfg: cfg, logger: logger, registry: registry}
}

// =============================================================================
// JWT 工具
// =============================================================================

// GenerateToken 生成 JWT。
func (m *Module) GenerateToken(username string) (string, time.Time, error) {
	exp := time.Now().Add(m.cfg.TokenTTL)
	claims := Claims{
		Username: username,
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "xiaozhi-server-go",
			Subject:   username,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(m.cfg.JWTSecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

// ParseToken 解析并校验 JWT。
func (m *Module) ParseToken(tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(m.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// =============================================================================
// 中间件
// =============================================================================

// JWTAuth 校验 Authorization: Bearer <jwt>。
func (m *Module) JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing or invalid Authorization header",
			})
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := m.ParseToken(tokenStr)
		if err != nil {
			m.logger.WarnContext(c.Request.Context(), "admin jwt invalid", "error", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("invalid token: %v", err),
			})
			return
		}
		c.Set("admin_username", claims.Username)
		c.Set("admin_role", claims.Role)
		c.Next()
	}
}

// =============================================================================
// HTTP 处理器
// =============================================================================

// LoginRequest 登录请求体。
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应。
type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	Username  string    `json:"username"`
}

// HandleLogin 处理 POST /api/admin/auth/login。
func (m *Module) HandleLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid body: %v", err)})
		return
	}
	if req.Username != m.cfg.Username {
		m.logger.WarnContext(c.Request.Context(), "admin login failed: bad username", "username", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(m.cfg.PasswordHash), []byte(req.Password)); err != nil {
		m.logger.WarnContext(c.Request.Context(), "admin login failed: bad password", "username", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, exp, err := m.GenerateToken(req.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("sign token: %v", err)})
		return
	}
	m.logger.InfoContext(c.Request.Context(), "admin login success", "username", req.Username)
	c.JSON(http.StatusOK, LoginResponse{
		Token:     token,
		ExpiresAt: exp,
		Username:  req.Username,
	})
}

// HandleMe 处理 GET /api/admin/auth/me（返回当前登录用户）。
func (m *Module) HandleMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"username": c.GetString("admin_username"),
		"role":     c.GetString("admin_role"),
	})
}

// ActivateByCodeRequest 通过激活码激活。
type ActivateByCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

// ActivateByCodeResponse 响应。
type ActivateByCodeResponse struct {
	DeviceID    string `json:"deviceId"`
	ActivatedAt int64  `json:"activatedAt"`
}

// HandleActivateByCode 处理 POST /api/admin/devices/activate-by-code。
func (m *Module) HandleActivateByCode(c *gin.Context) {
	var req ActivateByCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid body: %v", err)})
		return
	}
	deviceID, err := m.registry.ActivateByCode(c.Request.Context(), req.Code)
	if err != nil {
		m.logger.WarnContext(c.Request.Context(), "admin activate by code failed", "code", req.Code, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m.logger.InfoContext(c.Request.Context(), "admin activated device", "device_id", deviceID, "code", req.Code, "by", c.GetString("admin_username"))
	c.JSON(http.StatusOK, ActivateByCodeResponse{
		DeviceID:    deviceID,
		ActivatedAt: time.Now().Unix(),
	})
}

// HandleListPending 处理 GET /api/admin/devices/pending。
func (m *Module) HandleListPending(c *gin.Context) {
	list, err := m.registry.ListPending(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list, "count": len(list)})
}

// HandleListActivated 处理 GET /api/admin/devices/activated。
func (m *Module) HandleListActivated(c *gin.Context) {
	list, err := m.registry.ListActivated(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list, "count": len(list)})
}

// =============================================================================
// 路由注册
// =============================================================================

// RegisterRoutes 把 admin 模块路由挂到指定 gin.IRouter（/api/admin 前缀）。
func (m *Module) RegisterRoutes(r gin.IRouter) {
	r.POST("/auth/login", m.HandleLogin)

	auth := r.Group("/", m.JWTAuth())
	auth.GET("/auth/me", m.HandleMe)
	auth.POST("/devices/activate-by-code", m.HandleActivateByCode)
	auth.GET("/devices/pending", m.HandleListPending)
	auth.GET("/devices/activated", m.HandleListActivated)
}