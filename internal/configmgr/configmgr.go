// Package configmgr 实现系统配置管理。
//
// 核心职责：
//   - 配置项的 CRUD（LLM/STT/TTS/System 分类）
//   - 按分类查询配置
//   - 配置导入导出
//   - 多租户隔离
//
// 配置分类：
//   - llm：大语言模型配置（API Key、Base URL、Model Name 等）
//   - stt：语音识别配置
//   - tts：语音合成配置
//   - system：系统级配置（限流、日志级别等）
//
// 线程安全：所有公共方法受 sync.RWMutex 保护。
package configmgr

import (
	"context"
	"time"
)

// Manager 系统配置管理接口。
//
// 职责：
//   - 配置项的 CRUD
//   - 按分类（llm/stt/tts/system）查询
//   - 配置变更通知（Redis Pub/Sub）
//
// 配置分类：
//   - llm：大语言模型配置（API Key、Base URL、Model Name 等）
//   - stt：语音识别配置
//   - tts：语音合成配置
//   - system：系统级配置（限流、日志级别等）
//
// 对齐 Java ConfigAppService + ConfigService 逻辑。
type Manager interface {
	// Get 根据 key 获取配置项。
	//
	// 返回：配置项，不存在时返回 ErrConfigNotFound。
	Get(ctx context.Context, key string) (*ConfigItem, error)

	// Set 设置配置项（存在则更新，不存在则创建）。
	//
	// 配置变更后自动发布 Redis Pub/Sub 通知。
	Set(ctx context.Context, key string, req SetConfigReq) (*ConfigItem, error)

	// List 按条件查询配置项列表。
	List(ctx context.Context, filter ConfigFilter) (*ConfigPage, error)

	// Delete 删除配置项。
	Delete(ctx context.Context, key string) error

	// BatchGet 批量获取配置项。
	BatchGet(ctx context.Context, keys []string) (map[string]*ConfigItem, error)

	// GetByCategory 获取指定分类下所有配置项。
	GetByCategory(ctx context.Context, category string) ([]*ConfigItem, error)

	// Export 导出所有配置（用于备份或迁移）。
	Export(ctx context.Context, category string) (map[string]any, error)

	// Import 批量导入配置（用于恢复或迁移）。
	//
	// 模式：
	//   - "merge"：合并（已存在的跳过）
	//   - "overwrite"：覆盖（已存在的更新）
	Import(ctx context.Context, configs map[string]SetConfigReq, mode string) error
}

// ConfigItem 配置项。
type ConfigItem struct {
	Key       string    `json:"key"`       // 配置键（唯一，如 "llm.default.model"）
	Value     any       `json:"value"`     // 配置值（string/int/float/bool/json）
	Type      string    `json:"type"`      // 值类型：string / int / float / bool / json
	Category  string    `json:"category"`  // 分类：llm / stt / tts / system
	Label     string    `json:"label"`     // 配置项中文标签
	Remark    string    `json:"remark"`    // 备注说明
	TenantID  string    `json:"tenantId"`  // 租户 ID（空字符串表示全局配置）
	IsDefault bool      `json:"isDefault"` // 是否为默认配置
	IsSystem  bool      `json:"isSystem"`  // 是否为系统配置（不可删除）
	CreatedAt time.Time `json:"createdAt"` // 创建时间
	UpdatedAt time.Time `json:"updatedAt"` // 更新时间
}

// SetConfigReq 设置配置请求。
type SetConfigReq struct {
	Value     any    `json:"value"`     // 配置值（必填）
	Type      string `json:"type"`      // 值类型（必填）
	Category  string `json:"category"`  // 分类（必填）
	Label     string `json:"label"`     // 中文标签
	Remark    string `json:"remark"`    // 备注
	TenantID  string `json:"tenantId"`  // 租户 ID
	IsDefault bool   `json:"isDefault"` // 是否默认
}

// ConfigFilter 配置查询过滤条件。
type ConfigFilter struct {
	Category  string `json:"category"`  // 分类过滤
	Keyword   string `json:"keyword"`   // 搜索关键词（key/label/remark）
	TenantID  string `json:"tenantId"`  // 租户过滤
	IsDefault *bool  `json:"isDefault"` // 是否默认过滤
	Page      int    `json:"page"`      // 页码
	PageSize  int    `json:"pageSize"`  // 每页条数
}

// ConfigPage 配置分页结果。
type ConfigPage struct {
	Items    []*ConfigItem `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	HasMore  bool          `json:"hasMore"`
}