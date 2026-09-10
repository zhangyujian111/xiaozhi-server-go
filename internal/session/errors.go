package session

import "errors"

// 哨兵错误。
var (
	// ErrSessionNotFound 会话不存在。
	ErrSessionNotFound = errors.New("session: not found")

	// ErrSessionConflict 设备已有活跃会话，不可并发创建。
	ErrSessionConflict = errors.New("session: device already has active session")

	// ErrInvalidStateTransition 非法状态转移。
	ErrInvalidStateTransition = errors.New("session: invalid state transition")
)