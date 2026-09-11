// Package config 提供基于 viper 的配置加载功能。
//
// 配置优先级（从高到低）：
//  1. 环境变量（XZ_SERVER_PORT, XZ_AISAAS_URL 等）
//  2. config.yaml 配置文件
//  3. 默认值
//
// 用法：
//
//	cfg, err := config.Load("configs/config.yaml")
//	if err != nil { ... }
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用顶层配置。
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	WebSocket     WebSocketConfig     `mapstructure:"websocket"`
	Aisaas        AisaasConfig        `mapstructure:"aisaas"`
	Device        DeviceConfig        `mapstructure:"device"`
	Redis         RedisConfig         `mapstructure:"redis"`
	MySQL         MySQLConfig         `mapstructure:"mysql"`
	Log           LogConfig           `mapstructure:"log"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	MQTT          MQTTConfig          `mapstructure:"mqtt"`
	Vision        VisionConfig        `mapstructure:"vision"`
	Admin         AdminConfig         `mapstructure:"admin"`
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host           string        `mapstructure:"host"`
	Port           int           `mapstructure:"port"`
	Addr           string        `mapstructure:"addr"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	MaxHeaderBytes int           `mapstructure:"max_header_bytes"`
}

// EffectiveAddr 返回实际监听地址（优先使用 addr 字段，否则拼接 host:port）。
func (c ServerConfig) EffectiveAddr() string {
	if c.Addr != "" {
		return c.Addr
	}
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// WebSocketConfig WebSocket 协议配置。
type WebSocketConfig struct {
	ReadBufferSize   int           `mapstructure:"read_buffer_size"`
	WriteBufferSize  int           `mapstructure:"write_buffer_size"`
	MaxMessageSize   int64         `mapstructure:"max_message_size"`
	PingInterval     time.Duration `mapstructure:"ping_interval"`
	PongWait         time.Duration `mapstructure:"pong_wait"`
	WriteWait        time.Duration `mapstructure:"write_wait"`
	MaxConnections   int           `mapstructure:"max_connections"`
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
}

// AisaasConfig ykt-aisaas 后端服务配置。
type AisaasConfig struct {
	URL           string        `mapstructure:"url"`
	BaseURL       string        `mapstructure:"base_url"`
	InternalToken string        `mapstructure:"internal_token"`
	Timeout       time.Duration `mapstructure:"timeout"`
	MaxRetries    int           `mapstructure:"max_retries"`
	RetryBackoff  time.Duration `mapstructure:"retry_backoff"`
	// TLS mTLS 客户端配置（NICE v2，Phase 3 公网部署）。
	// 当三个文件路径均非空时，HTTP Transport 自动启用 mTLS。
	TLS AisaasTLSConfig `mapstructure:"tls"`
}

// AisaasTLSConfig mTLS 客户端配置。
type AisaasTLSConfig struct {
	ClientCertFile string `mapstructure:"client_cert_file"`
	ClientKeyFile  string `mapstructure:"client_key_file"`
	CACertFile     string `mapstructure:"ca_cert_file"`
	ServerName     string `mapstructure:"server_name"`
}

// EffectiveURL 返回 aisaas 地址（优先 url，否则 base_url）。
func (c AisaasConfig) EffectiveURL() string {
	if c.URL != "" {
		return strings.TrimRight(c.URL, "/")
	}
	return strings.TrimRight(c.BaseURL, "/")
}

// DeviceConfig 设备配置。
type DeviceConfig struct {
	MACID             string        `mapstructure:"mac_id"`
	EncryptedKeyFile  string        `mapstructure:"encrypted_key_file"`
	KeyRotateInterval time.Duration `mapstructure:"key_rotate_interval"`
	KeyRotateRequests int64         `mapstructure:"key_rotate_requests"`
}

// RedisConfig Redis 连接配置。
type RedisConfig struct {
	Addr         string        `mapstructure:"addr"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	PoolTimeout  time.Duration `mapstructure:"pool_timeout"`
	MaxRetries   int           `mapstructure:"max_retries"`
}

// MySQLConfig MySQL 连接配置。
type MySQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	Charset         string        `mapstructure:"charset"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	LogLevel        string        `mapstructure:"log_level"`
}

// DSN 返回 MySQL 连接字符串。
func (c MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.Database, c.Charset)
}

// LogConfig 日志配置。
type LogConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Output     string `mapstructure:"output"`
	FilePath   string `mapstructure:"file_path"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
	Compress   bool   `mapstructure:"compress"`
}

// ObservabilityConfig 可观测性配置。
type ObservabilityConfig struct {
	ServiceName    string        `mapstructure:"service_name"`
	ServiceVersion string        `mapstructure:"service_version"`
	Metrics        MetricsConfig `mapstructure:"metrics"`
	Tracing        TracingConfig `mapstructure:"tracing"`
	OTel           OTelConfig    `mapstructure:"otel"`
}

// MetricsConfig Prometheus metrics 配置。
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// TracingConfig OpenTelemetry tracing 配置。
type TracingConfig struct {
	Enabled    bool    `mapstructure:"enabled"`
	Exporter   string  `mapstructure:"exporter"`
	Endpoint   string  `mapstructure:"endpoint"`
	SampleRate float64 `mapstructure:"sample_rate"`
}

// OTelConfig OpenTelemetry metrics + logs 配置（V4-O 阶段）。
type OTelConfig struct {
	ExporterURL    string `mapstructure:"exporter_url"`    // "otel-collector:4317"
	MetricsEnabled bool   `mapstructure:"metrics_enabled"` // 默认 true
	LogsEnabled    bool   `mapstructure:"logs_enabled"`    // 默认 true
	ExportInterval int    `mapstructure:"export_interval"` // 默认 15s
}

// MQTTConfig MQTT 协议配置。
type MQTTConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	Broker        string        `mapstructure:"broker"`
	ClientID      string        `mapstructure:"client_id"`
	Username      string        `mapstructure:"username"`
	Password      string        `mapstructure:"password"`
	QoS           byte          `mapstructure:"qos"`
	KeepAlive     time.Duration `mapstructure:"keep_alive"`
	CleanSession  bool          `mapstructure:"clean_session"`
	AutoReconnect bool          `mapstructure:"auto_reconnect"`
}

// VisionConfig 视觉跟踪配置。
type VisionConfig struct {
	Enabled         bool           `mapstructure:"enabled"`
	CameraFPS       int            `mapstructure:"camera_fps"`        // 默认 5
	FollowGapMs     int            `mapstructure:"follow_gap_ms"`     // 220
	DeadZonePx      int            `mapstructure:"dead_zone_px"`       // 8 (范围 8-20)
	HFovDeg         float64        `mapstructure:"hfov_deg"`         // 65
	MinPulseUs      int            `mapstructure:"min_pulse_us"`      // 500
	MaxPulseUs      int            `mapstructure:"max_pulse_us"`      // 2500
	CenterPanUs     int            `mapstructure:"center_pan_us"`     // 1500
	CenterTiltUs    int            `mapstructure:"center_tilt_us"`    // 1500
	RangePanDeg     float64        `mapstructure:"range_pan_deg"`    // 90
	RangeTiltDeg    float64        `mapstructure:"range_tilt_deg"`   // 60
	InvertPan       bool           `mapstructure:"invert_pan"`
	InvertTilt      bool           `mapstructure:"invert_tilt"`
	SessionSecret   string         `mapstructure:"session_secret"`   // HMAC secret
	SessionTTL      time.Duration  `mapstructure:"session_ttl"`      // 1h
	AisaasBaseURL   string         `mapstructure:"aisaas_base_url"`  // 空则用 aisaas.url
	RequireTLS      bool           `mapstructure:"require_tls"`      // 强制 wss://
	PresharedTokens []DeviceToken  `mapstructure:"preshared_tokens"` // dev 硬编码
}

// DeviceToken 设备预共享 token 配置。
type DeviceToken struct {
	DeviceID string `mapstructure:"device_id"`
	Token    string `mapstructure:"token"`
}

// AdminConfig 后台管理 (xiaozhi-admin) 鉴权配置。
//
// Username / PasswordHash 用于 /api/admin/auth/login 校验；
// JWTSecret 用于签发/校验 JWT（HS256，必须 ≥ 32 字节以满足加密强度）；
// TokenTTL 控制 JWT 有效期（默认 24h）。
type AdminConfig struct {
	Username     string        `mapstructure:"username"`
	Password     string        `mapstructure:"password"`     // 明文（可选），启动时若 PasswordHash 为空则自动 bcrypt
	PasswordHash string        `mapstructure:"password_hash"` // bcrypt 哈希（优先）
	JWTSecret    string        `mapstructure:"jwt_secret"`
	TokenTTL     time.Duration `mapstructure:"token_ttl"`
}

// DefaultConfig 返回带默认值的配置。
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:           "0.0.0.0",
			Port:           8080,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   30 * time.Second,
			MaxHeaderBytes: 1 << 20, // 1MB
		},
		WebSocket: WebSocketConfig{
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
			MaxMessageSize:   65536,
			PingInterval:     30 * time.Second,
			PongWait:         60 * time.Second,
			WriteWait:        10 * time.Second,
			MaxConnections:   1000,
			HandshakeTimeout: 10 * time.Second,
		},
		Aisaas: AisaasConfig{
			Timeout:      30 * time.Second,
			MaxRetries:   3,
			RetryBackoff: 1 * time.Second,
		},
		Device: DeviceConfig{
			EncryptedKeyFile:  "configs/device.key.enc",
			KeyRotateInterval: 24 * time.Hour,
			KeyRotateRequests: 10000,
		},
		Redis: RedisConfig{
			Addr:         "localhost:6379",
			DB:           0,
			PoolSize:     100,
			MinIdleConns: 10,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			PoolTimeout:  4 * time.Second,
			MaxRetries:   3,
		},
		MySQL: MySQLConfig{
			Host:            "localhost",
			Port:            3306,
			Charset:         "utf8mb4",
			MaxOpenConns:    100,
			MaxIdleConns:    10,
			ConnMaxLifetime: 3600 * time.Second,
			ConnMaxIdleTime: 600 * time.Second,
			LogLevel:        "warn",
		},
		Log: LogConfig{
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			FilePath:   "logs/xiaozhi-server-go.log",
			MaxSize:    100,
			MaxBackups: 7,
			MaxAge:     30,
			Compress:   true,
		},
		Observability: ObservabilityConfig{
			ServiceName:    "xiaozhi-server-go",
			ServiceVersion: "0.1.0",
			Metrics: MetricsConfig{
				Enabled: true,
				Port:    9090,
				Path:    "/metrics",
			},
			Tracing: TracingConfig{
				Enabled:    false,
				Exporter:   "otlp",
				Endpoint:   "localhost:4317",
				SampleRate: 0.1,
			},
			OTel: OTelConfig{
				ExporterURL:    "",
				MetricsEnabled: true,
				LogsEnabled:    true,
				ExportInterval: 15,
			},
		},
		MQTT: MQTTConfig{
			Enabled:       false,
			Broker:        "tcp://localhost:1883",
			ClientID:      "xiaozhi-server-go",
			QoS:           1,
			KeepAlive:     60 * time.Second,
			CleanSession:  true,
			AutoReconnect: true,
		},
		Vision: VisionConfig{
			Enabled:      false,
			CameraFPS:    5,
			FollowGapMs:  220,
			DeadZonePx:   8,
			HFovDeg:      65.0,
			MinPulseUs:   500,
			MaxPulseUs:   2500,
			CenterPanUs:  1500,
			CenterTiltUs: 1500,
			RangePanDeg:  90.0,
			RangeTiltDeg: 60.0,
			SessionSecret: "dev-secret-change-in-prod",
			SessionTTL:   1 * time.Hour,
			RequireTLS:   false,
		},
		Admin: AdminConfig{
			Username:     "admin",
			PasswordHash: "$2a$10$NaCw46Rb/Gip4ZLnwz.oteh2TPv6fRHIJEQox2QLWGgcZ6NJB1nDW", // bcrypt of "admin123"
			JWTSecret:    "change-me-in-prod-min-32-bytes-long-secret",
			TokenTTL:     24 * time.Hour,
		},
	}
}

// Load 从 YAML 文件加载配置。
//
// 加载顺序：
//  1. 设置默认值
//  2. 读取 configFile（YAML 格式）
//  3. 覆盖环境变量（XZ_ 前缀，如 XZ_SERVER_PORT=9090）
//
// 环境变量映射规则：
//   - 点分隔路径 → 下划线，大写：server.port → XZ_SERVER_PORT
//   - 嵌套路径：aisaas.url → XZ_AISAAS_URL
func Load(configFile string) (*Config, error) {
	v := viper.New()

	// 1. 设置默认值
	defaults := DefaultConfig()
	v.SetDefault("server", defaults.Server)
	v.SetDefault("websocket", defaults.WebSocket)
	v.SetDefault("aisaas", defaults.Aisaas)
	v.SetDefault("device", defaults.Device)
	v.SetDefault("redis", defaults.Redis)
	v.SetDefault("mysql", defaults.MySQL)
	v.SetDefault("log", defaults.Log)
	v.SetDefault("observability", defaults.Observability)
	v.SetDefault("mqtt", defaults.MQTT)
	v.SetDefault("vision", defaults.Vision)
	v.SetDefault("admin", defaults.Admin)

	// 2. 读取配置文件
	v.SetConfigFile(configFile)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		// 配置文件不存在时使用默认值（不报错）
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("config: read config file %s: %w", configFile, err)
		}
	}

	// 3. 环境变量覆盖（XZ_ 前缀）
	v.SetEnvPrefix("XZ")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 4. 反序列化
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// 5. 后处理：补全 addr
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = cfg.Server.EffectiveAddr()
	}

	return &cfg, nil
}

// Validate 校验配置合法性。
func (c *Config) Validate() error {
	var errs []string

	if c.Aisaas.EffectiveURL() == "" {
		errs = append(errs, "aisaas.url is required")
	}
	if c.Aisaas.InternalToken == "" {
		errs = append(errs, "aisaas.internal_token is required")
	}
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Sprintf("server.port %d out of range [1, 65535]", c.Server.Port))
	}
	if c.WebSocket.MaxConnections <= 0 {
		errs = append(errs, "websocket.max_connections must be > 0")
	}
	if c.Log.Level != "" {
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[c.Log.Level] {
			errs = append(errs, fmt.Sprintf("log.level %q must be one of: debug, info, warn, error", c.Log.Level))
		}
	}

	// Vision 配置校验
	if c.Vision.Enabled {
		if c.Vision.CameraFPS < 1 || c.Vision.CameraFPS > 10 {
			errs = append(errs, fmt.Sprintf("vision.camera_fps %d out of range [1, 10]", c.Vision.CameraFPS))
		}
		if c.Vision.FollowGapMs < 100 || c.Vision.FollowGapMs > 500 {
			errs = append(errs, fmt.Sprintf("vision.follow_gap_ms %d out of range [100, 500]", c.Vision.FollowGapMs))
		}
		if c.Vision.DeadZonePx < 8 || c.Vision.DeadZonePx > 20 {
			errs = append(errs, fmt.Sprintf("vision.dead_zone_px %d out of range [8, 20]", c.Vision.DeadZonePx))
		}
		if c.Vision.MinPulseUs >= c.Vision.MaxPulseUs {
			errs = append(errs, "vision.min_pulse_us must be less than vision.max_pulse_us")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// EffectiveAisaasBaseURL 返回 aisaas base URL（优先使用 vision 配置，否则用 aisaas 配置）。
func (c *Config) EffectiveAisaasBaseURL() string {
	if c.Vision.AisaasBaseURL != "" {
		return strings.TrimRight(c.Vision.AisaasBaseURL, "/")
	}
	return c.Aisaas.EffectiveURL()
}
