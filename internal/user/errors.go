package user

import "errors"

// 哨兵错误。
var (
	// ErrUserNotFound 用户不存在。
	ErrUserNotFound = errors.New("user: not found")

	// ErrUserExists 用户已存在（用户名或邮箱重复）。
	ErrUserExists = errors.New("user: already exists")

	// ErrAuthFailed 认证失败（用户名或密码错误）。
	ErrAuthFailed = errors.New("user: authentication failed")

	// ErrUserLocked 账号已被锁定（登录失败超限）。
	ErrUserLocked = errors.New("user: account locked")

	// ErrWeakPassword 密码不符合安全策略（长度不足）。
	ErrWeakPassword = errors.New("user: password does not meet policy")
)