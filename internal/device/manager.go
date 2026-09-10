package device

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
)

// ============================================================
// Manager — 设备 API Key 生命周期管理
// ============================================================
//
// 设备启动时的鉴权流程：
//  1. 尝试从加密文件加载 Credentials
//  2. 如不存在 → 调 aisaas RegisterDevice → 加密存储
//  3. 如存在 → 解密 → 验证 API Key（调 aisaas VerifyKey）
//  4. 验证失败 → 强制轮换（调 aisaas RotateKey）
//  5. 缓存 apiKey 到内存（不落盘明文）
//
// 后台轮换策略：
//   - 时间驱动：24h 定时自动轮换
//   - 用量驱动：累计 10K 请求时触发轮换
//   - 401 鉴权失败：由调用方检测后触发 RotateKey（本 Manager 不自动处理）

// Manager 管理设备 API Key 的加密存储、加载和轮换。
//
// 线程安全：所有公共方法均受读写锁保护。
// usageCount 使用 atomic.Int64 实现无锁计数。
type Manager struct {
	cfg      *Config
	keystore *KeyStore
	client   *aisaas.Client

	mu     sync.RWMutex
	creds  *Credentials // 当前凭证（nil 表示未注册）
	apiKey string        // 当前 API Key 明文（缓存，避免频繁读 creds）

	usageCount   atomic.Int64   // 无锁请求计数
	usageTrigger chan struct{}  // 10K 触发信号（cap=1，非阻塞）

	logger *slog.Logger
}

// NewManager 创建设备 Key 管理器。
//
// 参数：
//   - cfg: 设备配置（加密文件路径、efuse MAC、设备信息）
//   - client: aisaas 客户端（已配置 BaseURL + InternalToken）
//   - logger: slog 日志器（nil 则使用默认）
//
// 返回：Manager 实例，或 KeyStore 创建失败时返回 error
func NewManager(cfg *Config, client *aisaas.Client, logger *slog.Logger) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("device: config is nil")
	}
	if client == nil {
		return nil, fmt.Errorf("device: aisaas client is nil")
	}
	if cfg.EncryptedKeyFile == "" {
		cfg.EncryptedKeyFile = "configs/device.key.enc"
	}

	if logger == nil {
		logger = slog.Default()
	}

	ks, err := NewKeyStore(cfg.EncryptedKeyFile, cfg.EFUSEMAC, logger)
	if err != nil {
		return nil, fmt.Errorf("device: create keystore: %w", err)
	}

	return &Manager{
		cfg:          cfg,
		keystore:     ks,
		client:       client,
		usageTrigger: make(chan struct{}, 1), // cap=1，非阻塞发信号
		logger:       logger.With("component", "device.manager"),
	}, nil
}

// Start 执行设备鉴权流程：加载或注册 API Key。
//
// 流程：
//  1. 尝试从加密文件加载 Credentials
//  2. 文件不存在或解密失败 → 向 aisaas 注册新设备 → 加密保存
//  3. 文件存在 → 解密 → 验证 API Key 有效性
//  4. 验证失败 → 强制轮换 → 加密保存
//  5. 缓存 apiKey 到内存
//
// 返回 error 表示鉴权失败（无法注册或无法轮换），调用方应终止启动。
func (m *Manager) Start(ctx context.Context) error {
	// 添加 trace span
	ctx, span := otel.Tracer("device-manager").Start(ctx, "Manager.Start "+m.cfg.DeviceID)
	defer span.End()
	span.SetAttributes(attribute.String("device.id", m.cfg.DeviceID))

	m.logger.InfoContext(ctx, "starting device authentication",
		"deviceId", m.cfg.DeviceID,
		"keyFile", m.cfg.EncryptedKeyFile,
	)

	// 1. 尝试加载加密的 credentials
	creds, err := m.keystore.Load()
	if err != nil {
		// 文件不存在或解密失败 → 首次启动，申请新凭证
		m.logger.WarnContext(ctx, "key file load failed, requesting new credentials",
			"err", err,
		)

		creds, err = m.requestNewCredentials(ctx)
		if err != nil {
			return fmt.Errorf("device: request new credentials: %w", err)
		}

		if err := m.keystore.Save(creds); err != nil {
			return fmt.Errorf("device: save new credentials: %w", err)
		}

		m.logger.InfoContext(ctx, "new device credentials obtained",
			"deviceId", creds.DeviceID,
			"keyId", creds.KeyID,
		)
	} else {
		// 2. 文件存在 → 验证现有 key
		m.client.SetAPIKey(creds.APIKey)
		m.apiKey = creds.APIKey

		m.logger.InfoContext(ctx, "key file loaded, verifying api key",
			"deviceId", creds.DeviceID,
			"keyId", creds.KeyID,
		)

		if err := m.client.VerifyKey(ctx); err != nil {
			// 验证失败 → 强制轮换
			m.logger.WarnContext(ctx, "key verification failed, forcing rotation",
				"err", err,
				"keyId", creds.KeyID,
			)

			creds, err = m.rotateKey(ctx, creds.KeyID)
			if err != nil {
				return fmt.Errorf("device: force rotate after verify fail: %w", err)
			}

			if err := m.keystore.Save(creds); err != nil {
				return fmt.Errorf("device: save rotated credentials: %w", err)
			}

			m.apiKey = creds.APIKey
			m.client.SetAPIKey(creds.APIKey)

			m.logger.InfoContext(ctx, "key rotated after verification failure",
				"newKeyId", creds.KeyID,
			)
		}
	}

	// 3. 缓存到内存
	m.mu.Lock()
	m.creds = creds
	m.apiKey = creds.APIKey
	m.mu.Unlock()

	m.logger.InfoContext(ctx, "device manager started successfully",
		"deviceId", creds.DeviceID,
		"keyId", creds.KeyID,
	)
	return nil
}

// GetAPIKey 返回当前 API Key 明文（线程安全）。
//
// 用于业务层获取 API Key 构造 Authorization header。
// 返回空字符串表示尚未完成鉴权。
func (m *Manager) GetAPIKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.apiKey
}

// GetKeyID 返回当前 Key ID（线程安全）。
func (m *Manager) GetKeyID() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.creds == nil {
		return 0
	}
	return m.creds.KeyID
}

// IsRegistered 返回是否已完成鉴权。
func (m *Manager) IsRegistered() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.creds != nil
}

// IncrementUsage 递增请求计数（无锁，高性能）。
//
// 每 10K 请求触发一次 usageTrigger 信号，触发 RotateKeyWorker 轮换。
// 调用方应在每次对外 API 请求完成后调用此方法。
func (m *Manager) IncrementUsage() {
	count := m.usageCount.Add(1)
	if count%10000 == 0 {
		// 非阻塞发送信号（cap=1 的 channel，若已有未消费信号则跳过）
		select {
		case m.usageTrigger <- struct{}{}:
		default:
		}
	}
}

// UsageCount 返回当前累计请求计数。
func (m *Manager) UsageCount() int64 {
	return m.usageCount.Load()
}

// RotateKey 手动轮换 API Key 并更新加密文件。
//
// 调用 aisaas RotateKey 接口，旧 Key 保留 5 分钟宽限期。
// 轮换成功后自动更新内存缓存和加密文件。
func (m *Manager) RotateKey(ctx context.Context) error {
	m.mu.RLock()
	if m.creds == nil {
		m.mu.RUnlock()
		return ErrNotRegistered
	}
	keyID := m.creds.KeyID
	m.mu.RUnlock()

	m.logger.InfoContext(ctx, "manual key rotation triggered",
		"keyId", keyID,
	)

	creds, err := m.rotateKey(ctx, keyID)
	if err != nil {
		return fmt.Errorf("device: rotate key: %w", err)
	}

	if err := m.keystore.Save(creds); err != nil {
		return fmt.Errorf("device: save rotated key: %w", err)
	}

	m.mu.Lock()
	m.creds = creds
	m.apiKey = creds.APIKey
	m.mu.Unlock()

	m.client.SetAPIKey(creds.APIKey)

	m.logger.InfoContext(ctx, "api key rotated successfully",
		"newKeyId", creds.KeyID,
		"expiresAt", creds.ExpiresAt,
	)
	return nil
}

// RotateKeyWorker 后台 Key 轮换 worker。
//
// 两种触发方式：
//   - 时间驱动：每 24h 定时轮换
//   - 用量驱动：IncrementUsage 累计达到 10K 时触发
//
// 通过 ctx 取消时退出。
// 应在独立的 goroutine 中运行：go mgr.RotateKeyWorker(ctx)
func (m *Manager) RotateKeyWorker(ctx context.Context) {
	rotateInterval := 24 * time.Hour
	ticker := time.NewTicker(rotateInterval)
	defer ticker.Stop()

	m.logger.InfoContext(ctx, "key rotation worker started",
		"interval_hours", 24,
		"usage_threshold", 10000,
	)

	for {
		select {
		case <-ctx.Done():
			m.logger.InfoContext(ctx, "key rotation worker stopped")
			return

		case <-ticker.C:
			// 24h 定时轮换
			m.handleRotation(ctx, "time_24h")

		case <-m.usageTrigger:
			// 10K 请求触发轮换
			m.handleRotation(ctx, "usage_10k")
		}
	}
}

// ============================================================
// 内部方法
// ============================================================

// requestNewCredentials 向 aisaas 注册新设备并获取 API Key。
func (m *Manager) requestNewCredentials(ctx context.Context) (*Credentials, error) {
	deviceID := m.cfg.DeviceID
	if deviceID == "" {
		deviceID = m.cfg.EFUSEMAC
	}

	cred, err := m.client.RegisterDevice(ctx, deviceID, &aisaas.DeviceHWInfo{
		MAC:             m.cfg.EFUSEMAC,
		ChipType:        m.cfg.ChipType,
		FirmwareVersion: m.cfg.Firmware,
	})
	if err != nil {
		return nil, fmt.Errorf("device: register device %s: %w", deviceID, err)
	}

	// 注册成功后立即设置 API Key 到客户端
	m.client.SetAPIKey(cred.APIKey)

	return &Credentials{
		DeviceID:  cred.DeviceID,
		APIKey:    cred.APIKey,
		KeyID:     cred.KeyID,
		ExpiresAt: cred.ExpiresAt,
		CreatedAt: time.Now(),
	}, nil
}

// rotateKey 调用 aisaas RotateKey 并返回新凭证。
//
// 注意：此方法不持有锁，由调用方（Start/RotateKey/handleRotation）负责加锁。
func (m *Manager) rotateKey(ctx context.Context, keyID int64) (*Credentials, error) {
	cred, err := m.client.RotateKey(ctx, keyID, 1)
	if err != nil {
		return nil, fmt.Errorf("device: rotate key %d: %w", keyID, err)
	}

	return &Credentials{
		DeviceID:  cred.DeviceID,
		APIKey:    cred.APIKey,
		KeyID:     cred.KeyID,
		ExpiresAt: cred.ExpiresAt,
		CreatedAt: time.Now(),
	}, nil
}

// handleRotation 执行一次轮换并更新内存状态。
//
// 由 RotateKeyWorker 的两种触发路径调用。
// 持有写锁，确保线程安全。
func (m *Manager) handleRotation(ctx context.Context, trigger string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.creds == nil {
		m.logger.WarnContext(ctx, "rotation skipped: not registered",
			"trigger", trigger,
		)
		return
	}

	m.logger.InfoContext(ctx, "rotating api key",
		"trigger", trigger,
		"keyId", m.creds.KeyID,
		"usageCount", m.usageCount.Load(),
	)

	creds, err := m.rotateKey(ctx, m.creds.KeyID)
	if err != nil {
		m.logger.ErrorContext(ctx, "key rotation failed",
			"err", err,
			"trigger", trigger,
			"keyId", m.creds.KeyID,
		)
		return
	}

	if err := m.keystore.Save(creds); err != nil {
		m.logger.ErrorContext(ctx, "save rotated key failed",
			"err", err,
			"trigger", trigger,
		)
		// 即使保存失败，也更新内存中的 key（避免丢失新 key）
		// 但不视为完全成功
	}

	m.creds = creds
	m.apiKey = creds.APIKey
	m.client.SetAPIKey(creds.APIKey)

	m.logger.InfoContext(ctx, "api key rotated successfully",
		"trigger", trigger,
		"newKeyId", creds.KeyID,
		"expiresAt", creds.ExpiresAt,
	)
}