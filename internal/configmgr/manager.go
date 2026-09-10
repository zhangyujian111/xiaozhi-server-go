package configmgr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// managerImpl 内存配置管理器实现。
//
// 线程安全：所有公共方法受 mu 保护。
// 支持 viper 配置 fallback 作为默认值源。
type managerImpl struct {
	mu       sync.RWMutex
	configs  map[string]*ConfigItem // key → ConfigItem
	defaults  map[string]*ConfigItem // 默认配置（fallback）
	logger   *slog.Logger
}

// NewManager 创建内存配置管理器。
//
// 预加载默认 LLM/STT/TTS/System 配置项。
//
// 参数：
//   - logger：slog 日志器（nil 则使用默认）
//
// 返回：Manager 接口实例。
func NewManager(logger *slog.Logger) Manager {
	if logger == nil {
		logger = slog.Default()
	}

	m := &managerImpl{
		configs: make(map[string]*ConfigItem),
		defaults: make(map[string]*ConfigItem),
		logger:  logger.With("component", "configmgr.manager"),
	}

	// 加载默认配置
	m.seedDefaults()

	return m
}

// seedDefaults 预加载默认系统配置。
// 这些配置作为 fallback，当内存中没有对应 key 时返回默认值。
func (m *managerImpl) seedDefaults() {
	now := time.Now()

	defaultItems := []ConfigItem{
		// LLM 默认配置
		{Key: "llm.default.model", Value: "gpt-4o", Type: "string", Category: "llm", Label: "默认LLM模型", Remark: "默认使用的 LLM 模型名称", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "llm.default.base_url", Value: "https://api.openai.com/v1", Type: "string", Category: "llm", Label: "LLM API地址", Remark: "LLM 服务的基础 URL", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "llm.default.max_tokens", Value: 4096, Type: "int", Category: "llm", Label: "最大Token数", Remark: "单次请求最大输出 token 数", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "llm.default.temperature", Value: 0.7, Type: "float", Category: "llm", Label: "温度参数", Remark: "控制输出随机性，0-1 之间", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},

		// STT 默认配置
		{Key: "stt.default.model", Value: "whisper-1", Type: "string", Category: "stt", Label: "默认STT模型", Remark: "默认使用的语音识别模型", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "stt.default.language", Value: "zh-CN", Type: "string", Category: "stt", Label: "默认识别语言", Remark: "默认语音识别语言代码", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "stt.default.sample_rate", Value: 16000, Type: "int", Category: "stt", Label: "默认采样率", Remark: "音频采样率（Hz）", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},

		// TTS 默认配置
		{Key: "tts.default.model", Value: "tts-1", Type: "string", Category: "tts", Label: "默认TTS模型", Remark: "默认使用的语音合成模型", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "tts.default.voice", Value: "alloy", Type: "string", Category: "tts", Label: "默认TTS音色", Remark: "默认语音合成音色", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "tts.default.speed", Value: 1.0, Type: "float", Category: "tts", Label: "默认语速", Remark: "语音合成速度，1.0 为正常速度", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},

		// System 默认配置
		{Key: "system.session.idle_timeout", Value: "24h", Type: "string", Category: "system", Label: "会话空闲超时", Remark: "会话空闲超过此时间后自动关闭", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "system.rate_limit.enabled", Value: true, Type: "bool", Category: "system", Label: "启用限流", Remark: "是否启用 API 限流", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
		{Key: "system.rate_limit.qps", Value: 100, Type: "int", Category: "system", Label: "限流QPS", Remark: "每秒最大请求数", TenantID: "", IsDefault: true, IsSystem: true, CreatedAt: now, UpdatedAt: now},
	}

	for i := range defaultItems {
		item := &defaultItems[i]
		m.defaults[item.Key] = item
	}

	m.logger.Info("default configs seeded", "count", len(defaultItems))
}

// Get 根据 key 获取配置项。
//
// 查找顺序：内存存储 → 默认配置 → ErrConfigNotFound
func (m *managerImpl) Get(ctx context.Context, key string) (*ConfigItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 优先从内存存储查找
	if item, ok := m.configs[key]; ok {
		return copyConfigItem(item), nil
	}

	// 回退到默认配置
	if item, ok := m.defaults[key]; ok {
		m.logger.DebugContext(ctx, "config key not in memory, using default",
			"key", key,
		)
		return copyConfigItem(item), nil
	}

	return nil, ErrConfigNotFound
}

// Set 设置配置项（存在则更新，不存在则创建）。
func (m *managerImpl) Set(ctx context.Context, key string, req SetConfigReq) (*ConfigItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// 检查是否已存在
	if existing, ok := m.configs[key]; ok {
		// 更新现有配置
		existing.Value = req.Value
		existing.Type = req.Type
		existing.Category = req.Category
		if req.Label != "" {
			existing.Label = req.Label
		}
		if req.Remark != "" {
			existing.Remark = req.Remark
		}
		existing.TenantID = req.TenantID
		existing.IsDefault = req.IsDefault
		existing.UpdatedAt = now

		m.logger.InfoContext(ctx, "config updated",
			"key", key,
			"category", existing.Category,
		)
		return copyConfigItem(existing), nil
	}

	// 创建新配置项
	item := &ConfigItem{
		Key:       key,
		Value:     req.Value,
		Type:      req.Type,
		Category:  req.Category,
		Label:     req.Label,
		Remark:    req.Remark,
		TenantID:  req.TenantID,
		IsDefault: req.IsDefault,
		IsSystem:  false, // 手动创建的配置不是系统配置
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.configs[key] = item

	m.logger.InfoContext(ctx, "config created",
		"key", key,
		"category", item.Category,
	)

	return copyConfigItem(item), nil
}

// List 按条件查询配置项列表。
func (m *managerImpl) List(ctx context.Context, filter ConfigFilter) (*ConfigPage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 设置默认分页参数
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	// 收集所有配置（内存存储 + 默认配置合并）
	allConfigs := make(map[string]*ConfigItem)
	for k, v := range m.defaults {
		allConfigs[k] = v
	}
	for k, v := range m.configs {
		allConfigs[k] = v
	}

	// 过滤匹配的配置项
	var matched []*ConfigItem
	for _, item := range allConfigs {
		// 分类过滤
		if filter.Category != "" && item.Category != filter.Category {
			continue
		}

		// 关键词过滤
		if filter.Keyword != "" {
			kw := strings.ToLower(filter.Keyword)
			if !strings.Contains(strings.ToLower(item.Key), kw) &&
				!strings.Contains(strings.ToLower(item.Label), kw) &&
				!strings.Contains(strings.ToLower(item.Remark), kw) {
				continue
			}
		}

		// 租户过滤
		if filter.TenantID != "" && item.TenantID != filter.TenantID {
			continue
		}

		// 是否默认过滤
		if filter.IsDefault != nil && item.IsDefault != *filter.IsDefault {
			continue
		}

		matched = append(matched, copyConfigItem(item))
	}

	total := int64(len(matched))

	// 分页
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(matched) {
		return &ConfigPage{
			Items:    []*ConfigItem{},
			Total:    total,
			Page:     filter.Page,
			PageSize: filter.PageSize,
			HasMore:  false,
		}, nil
	}

	end := start + filter.PageSize
	if end > len(matched) {
		end = len(matched)
	}

	return &ConfigPage{
		Items:    matched[start:end],
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		HasMore:  end < len(matched),
	}, nil
}

// Delete 删除配置项（系统配置不可删除）。
func (m *managerImpl) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否为系统默认配置
	if defItem, ok := m.defaults[key]; ok && defItem.IsSystem {
		return ErrConfigSystem
	}

	item, ok := m.configs[key]
	if !ok {
		return ErrConfigNotFound
	}

	if item.IsSystem {
		return ErrConfigSystem
	}

	delete(m.configs, key)

	m.logger.InfoContext(ctx, "config deleted",
		"key", key,
	)

	return nil
}

// BatchGet 批量获取配置项。
func (m *managerImpl) BatchGet(ctx context.Context, keys []string) (map[string]*ConfigItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*ConfigItem, len(keys))
	for _, key := range keys {
		// 优先内存存储
		if item, ok := m.configs[key]; ok {
			result[key] = copyConfigItem(item)
		} else if item, ok := m.defaults[key]; ok {
			// 回退到默认配置
			result[key] = copyConfigItem(item)
		}
		// 不存在则跳过（不返回错误）
	}

	return result, nil
}

// GetByCategory 获取指定分类下所有配置项。
func (m *managerImpl) GetByCategory(ctx context.Context, category string) ([]*ConfigItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 合并默认配置和内存存储
	merged := make(map[string]*ConfigItem)
	for k, v := range m.defaults {
		merged[k] = v
	}
	for k, v := range m.configs {
		merged[k] = v
	}

	var result []*ConfigItem
	for _, item := range merged {
		if item.Category == category {
			result = append(result, copyConfigItem(item))
		}
	}

	return result, nil
}

// Export 导出所有配置（用于备份或迁移）。
//
// 参数：
//   - category：分类过滤，空字符串表示导出全部
//
// 返回：key → value 的映射。
func (m *managerImpl) Export(ctx context.Context, category string) (map[string]any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 合并默认配置和内存存储
	merged := make(map[string]*ConfigItem)
	for k, v := range m.defaults {
		merged[k] = v
	}
	for k, v := range m.configs {
		merged[k] = v
	}

	result := make(map[string]any)
	for key, item := range merged {
		if category == "" || item.Category == category {
			result[key] = item.Value
		}
	}

	return result, nil
}

// Import 批量导入配置（用于恢复或迁移）。
//
// 模式：
//   - "merge"：合并（已存在的跳过）
//   - "overwrite"：覆盖（已存在的更新）
func (m *managerImpl) Import(ctx context.Context, configs map[string]SetConfigReq, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	for key, req := range configs {
		if existing, ok := m.configs[key]; ok {
			switch mode {
			case "merge":
				// 合并模式：跳过已存在的
				continue
			case "overwrite":
				// 覆盖模式：更新现有配置
				existing.Value = req.Value
				existing.Type = req.Type
				existing.Category = req.Category
				if req.Label != "" {
					existing.Label = req.Label
				}
				if req.Remark != "" {
					existing.Remark = req.Remark
				}
				existing.TenantID = req.TenantID
				existing.IsDefault = req.IsDefault
				existing.UpdatedAt = now
			default:
				return fmt.Errorf("configmgr: unknown import mode: %s", mode)
			}
		} else {
			// 不存在则创建
			m.configs[key] = &ConfigItem{
				Key:       key,
				Value:     req.Value,
				Type:      req.Type,
				Category:  req.Category,
				Label:     req.Label,
				Remark:    req.Remark,
				TenantID:  req.TenantID,
				IsDefault: req.IsDefault,
				IsSystem:  false,
				CreatedAt: now,
				UpdatedAt: now,
			}
		}
	}

	m.logger.InfoContext(ctx, "config imported",
		"count", len(configs),
		"mode", mode,
	)

	return nil
}

// copyConfigItem 深拷贝配置项。
func copyConfigItem(item *ConfigItem) *ConfigItem {
	cp := *item
	return &cp
}