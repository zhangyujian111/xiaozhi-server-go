package session

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// managerImpl 内存会话管理器实现。
//
// 线程安全：所有公共方法受 mu 保护。
// Cleanup worker 在独立 goroutine 中运行。
type managerImpl struct {
	mu       sync.RWMutex
	sessions map[string]*Session // sessionID → Session
	logger   *slog.Logger
}

// NewManager 创建内存会话管理器。
//
// 参数：
//   - logger：slog 日志器（nil 则使用默认）
//
// 返回：Manager 接口实例。
func NewManager(logger *slog.Logger) Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &managerImpl{
		sessions: make(map[string]*Session),
		logger:   logger.With("component", "session.manager"),
	}
}

// Start 创建新会话并进入 Active 状态。
func (m *managerImpl) Start(ctx context.Context, deviceID, personaID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查同一设备是否已有 Active 会话
	for _, s := range m.sessions {
		if s.DeviceID == deviceID && s.State == StateActive {
			return nil, ErrSessionConflict
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("session: generate uuid: %w", err)
	}

	now := time.Now()
	sess := &Session{
		ID:         id.String(),
		DeviceID:   deviceID,
		PersonaID:  personaID,
		State:      StateActive,
		StartedAt:  now,
		LastActive: now,
		EndedAt:    nil,
		QuotaUsed:  QuotaUsage{},
	}

	m.sessions[sess.ID] = sess

	m.logger.InfoContext(ctx, "session started",
		"sessionId", sess.ID,
		"deviceId", deviceID,
		"personaId", personaID,
	)

	return sess, nil
}

// Get 根据会话 ID 查询会话。
func (m *managerImpl) Get(ctx context.Context, sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	// 返回副本，避免外部修改
	cp := *sess
	return &cp, nil
}

// UpdateState 更新会话状态。
func (m *managerImpl) UpdateState(ctx context.Context, sessionID string, state State) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}

	// 验证状态转移合法性
	if !isValidTransition(sess.State, state) {
		m.logger.WarnContext(ctx, "invalid state transition",
			"sessionId", sessionID,
			"from", sess.State.String(),
			"to", state.String(),
		)
		return ErrInvalidStateTransition
	}

	oldState := sess.State
	sess.State = state
	sess.LastActive = time.Now()

	// 如果转移到 Closed，记录结束时间
	if state == StateClosed {
		now := time.Now()
		sess.EndedAt = &now
	}

	m.logger.InfoContext(ctx, "session state updated",
		"sessionId", sessionID,
		"from", oldState.String(),
		"to", state.String(),
	)

	return nil
}

// List 列出指定设备的所有会话（按创建时间倒序）。
func (m *managerImpl) List(ctx context.Context, filter SessionFilter) (*SessionPage, error) {
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

	// 收集匹配的会话
	var matched []*Session
	for _, s := range m.sessions {
		// 设备 ID 过滤
		if filter.DeviceID != "" && s.DeviceID != filter.DeviceID {
			continue
		}
		// 状态过滤
		if filter.State != nil && s.State != *filter.State {
			continue
		}
		cp := *s
		matched = append(matched, &cp)
	}

	// 按创建时间倒序排列
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].StartedAt.After(matched[j].StartedAt)
	})

	total := int64(len(matched))

	// 分页
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(matched) {
		return &SessionPage{
			Items:    []*Session{},
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

	return &SessionPage{
		Items:    matched[start:end],
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		HasMore:  end < len(matched),
	}, nil
}

// End 正常结束会话（Active/Idle → Closed）。
func (m *managerImpl) End(ctx context.Context, sessionID string) (*QuotaUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	if sess.State != StateActive && sess.State != StateIdle {
		return nil, ErrInvalidStateTransition
	}

	now := time.Now()
	sess.State = StateClosed
	sess.EndedAt = &now
	sess.LastActive = now

	quota := sess.QuotaUsed

	m.logger.InfoContext(ctx, "session ended",
		"sessionId", sessionID,
		"quotaUsed", quota,
	)

	return &quota, nil
}

// Cleanup 清理超时会话。
//
// 扫描所有 Idle 会话，关闭 lastActive 超过 idleTimeout 的会话。
// 返回清理的会话数量。
func (m *managerImpl) Cleanup(ctx context.Context, idleTimeout time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-idleTimeout)
	var cleaned int

	for id, sess := range m.sessions {
		if sess.State == StateIdle && sess.LastActive.Before(cutoff) {
			sess.State = StateClosed
			sess.EndedAt = &now
			sess.LastActive = now

			m.logger.InfoContext(ctx, "session cleaned up (idle timeout)",
				"sessionId", id,
				"deviceId", sess.DeviceID,
				"lastActive", sess.LastActive,
				"idleDuration", now.Sub(sess.LastActive),
			)
			cleaned++
		}
	}

	return cleaned, nil
}

// StartCleanupWorker 启动后台清理 worker。
//
// 参数：
//   - ctx：用于取消 worker 的 context
//   - interval：清理扫描间隔（如 5 分钟）
//   - idleTimeout：空闲超时时间（如 24 小时）
//
// 应在独立 goroutine 中运行：go mgr.StartCleanupWorker(ctx, 5*time.Minute, 24*time.Hour)
func (m *managerImpl) StartCleanupWorker(ctx context.Context, interval, idleTimeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.logger.InfoContext(ctx, "session cleanup worker started",
		"interval", interval,
		"idleTimeout", idleTimeout,
	)

	for {
		select {
		case <-ctx.Done():
			m.logger.InfoContext(ctx, "session cleanup worker stopped")
			return
		case <-ticker.C:
			cleaned, err := m.Cleanup(ctx, idleTimeout)
			if err != nil {
				m.logger.ErrorContext(ctx, "session cleanup failed", "error", err)
			} else if cleaned > 0 {
				m.logger.InfoContext(ctx, "session cleanup completed",
					"cleaned", cleaned,
				)
			}
		}
	}
}

// isValidTransition 验证状态转移是否合法。
func isValidTransition(from, to State) bool {
	switch from {
	case StateInit:
		return to == StateActive
	case StateActive:
		return to == StateIdle || to == StateClosed
	case StateIdle:
		return to == StateActive || to == StateClosed
	case StateClosed:
		return false // 已关闭的会话不可再转移
	default:
		return false
	}
}