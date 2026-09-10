// Package session 实现设备会话生命周期管理。
//
// 核心职责：
//   - 会话状态机（Init → Active → Idle → Closed）
//   - 会话创建与销毁
//   - 配额追踪（LLM tokens / TTS chars / ASR seconds）
//   - 超时会话自动清理
//
// 线程安全：所有公共方法受 sync.RWMutex 保护。
package session

import (
	"context"
	"time"
)

// State 会话状态枚举。
type State int

const (
	StateInit   State = 0 // 初始态（握手完成，等待激活）
	StateActive State = 1 // 活跃态（正在对话）
	StateIdle   State = 2 // 空闲态（无对话，等待超时）
	StateClosed State = 3 // 已关闭（主动结束或超时清理）
)

// String 返回状态的可读名称。
func (s State) String() string {
	switch s {
	case StateInit:
		return "init"
	case StateActive:
		return "active"
	case StateIdle:
		return "idle"
	case StateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Manager 设备会话管理器接口。
//
// 职责：
//   - 会话创建与销毁
//   - 状态转移（Init → Active → Idle → Closed）
//   - 会话超时清理
//   - 配额追踪（与 ykt-aisaas session 服务同步）
//
// 实现约束：
//   - 线程安全（所有方法须受锁保护）
//   - 会话状态变更需记录审计日志
//   - Cleanup 应在独立 goroutine 中定时执行
type Manager interface {
	// Start 创建新会话并进入 Active 状态。
	//
	// 参数：
	//   - deviceID：设备唯一标识
	//   - personaID：关联的人设 ID（可选，空字符串表示使用默认人设）
	//
	// 返回：创建的会话对象，或错误。
	// 同一设备不可同时存在多个 Active 会话（并发创建返回 ErrSessionConflict）。
	Start(ctx context.Context, deviceID, personaID string) (*Session, error)

	// Get 根据会话 ID 查询会话。
	//
	// 返回：会话对象，不存在时返回 ErrSessionNotFound。
	Get(ctx context.Context, sessionID string) (*Session, error)

	// UpdateState 更新会话状态。
	//
	// 仅允许合法状态转移：
	//   - Init → Active
	//   - Active → Idle
	//   - Idle → Active（用户恢复交互）
	//   - Active/Idle → Closed
	// 非法转移返回 ErrInvalidStateTransition。
	UpdateState(ctx context.Context, sessionID string, state State) error

	// List 列出指定设备的所有会话（按创建时间倒序）。
	//
	// 参数：
	//   - filter：分页与过滤条件
	//
	// 返回：会话列表与分页信息。
	List(ctx context.Context, filter SessionFilter) (*SessionPage, error)

	// End 正常结束会话（Active/Idle → Closed）。
	//
	// 结束时触发配额结算（调用 aisaas session end 接口）。
	// 返回结算后的配额使用量。
	End(ctx context.Context, sessionID string) (*QuotaUsage, error)

	// Cleanup 清理超时会话。
	//
	// 参数：
	//   - idleTimeout：空闲超时时间（超过此时间未活跃的 Idle 会话将被关闭）
	//
	// 返回：清理的会话数量。
	// 应在独立 goroutine 中定时调用（如每 5 分钟）。
	Cleanup(ctx context.Context, idleTimeout time.Duration) (int, error)
}

// Session 会话对象。
type Session struct {
	ID         string     `json:"id"`          // 会话唯一标识（UUID v7）
	DeviceID   string     `json:"deviceId"`     // 设备 ID
	PersonaID  string     `json:"personaId"`    // 人设 ID（空字符串表示默认）
	State      State      `json:"state"`        // 当前状态
	StartedAt  time.Time  `json:"startedAt"`    // 创建时间
	LastActive time.Time  `json:"lastActive"`   // 最后活跃时间
	EndedAt    *time.Time `json:"endedAt"`      // 结束时间（null 表示未结束）
	QuotaUsed  QuotaUsage `json:"quotaUsed"`    // 已消耗配额
}

// QuotaUsage 配额使用量（多维度）。
type QuotaUsage struct {
	LLMTokensIn  int64 `json:"llmTokensIn"`  // LLM 输入 token 数
	LLMTokensOut int64 `json:"llmTokensOut"` // LLM 输出 token 数
	TTSChars     int64 `json:"ttsChars"`     // TTS 合成字符数
	ASRSeconds   int64 `json:"asrSeconds"`   // ASR 识别秒数
}

// SessionFilter 会话查询过滤条件。
type SessionFilter struct {
	DeviceID string `json:"deviceId"` // 设备 ID（必填）
	State    *State `json:"state"`    // 会话状态过滤（nil 表示全部）
	Page     int    `json:"page"`     // 页码（1-based，默认 1）
	PageSize int    `json:"pageSize"` // 每页条数（默认 20，最大 100）
}

// SessionPage 会话分页结果。
type SessionPage struct {
	Items    []*Session `json:"items"`    // 当前页会话列表
	Total    int64      `json:"total"`    // 总记录数
	Page     int        `json:"page"`     // 当前页码
	PageSize int        `json:"pageSize"` // 每页条数
	HasMore  bool       `json:"hasMore"`  // 是否有下一页
}