// Package user 实现用户账号管理与认证。
//
// 核心职责：
//   - 用户 CRUD（创建、查询、更新、软删除）
//   - 密码认证（BCrypt cost ≥ 12）
//   - 角色分配
//   - 登录状态记录与锁定策略
//
// 安全约束：
//   - 密码存储使用 BCrypt（cost ≥ 12），禁止明文存储
//   - 登录失败 5 次后锁定 15 分钟
//   - Delete 为软删除（设置 deleted_at 时间戳）
//
// 线程安全：所有公共方法受 sync.RWMutex 保护。
package user

import (
	"context"
	"time"
)

// Manager 用户管理接口。
//
// 职责：
//   - 用户 CRUD
//   - 密码认证（BCrypt cost ≥ 12）
//   - 角色分配
//   - 登录状态记录
//
// 安全约束：
//   - 密码存储使用 BCrypt（cost ≥ 12），禁止明文存储
//   - 登录失败 5 次后锁定 15 分钟
//   - Delete 为软删除（设置 deleted_at 时间戳）
type Manager interface {
	// Create 创建新用户。
	//
	// 密码在创建时自动 BCrypt 加密。
	// 用户名/邮箱全局唯一，重复返回 ErrUserExists。
	Create(ctx context.Context, req CreateUserReq) (*User, error)

	// Get 根据用户 ID 查询用户。
	//
	// 返回：用户对象，不存在时返回 ErrUserNotFound。
	Get(ctx context.Context, userID string) (*User, error)

	// GetByUsername 根据用户名查询用户。
	GetByUsername(ctx context.Context, username string) (*User, error)

	// Update 更新用户信息。
	//
	// 仅更新非零值字段。密码通过 UpdatePassword 单独更新。
	Update(ctx context.Context, userID string, req UpdateUserReq) (*User, error)

	// UpdatePassword 更新用户密码（BCrypt 加密）。
	//
	// 需验证旧密码（oldPassword）正确后才能更新。
	UpdatePassword(ctx context.Context, userID, oldPassword, newPassword string) error

	// Delete 软删除用户（设置 deleted_at）。
	Delete(ctx context.Context, userID string) error

	// List 分页查询用户列表。
	List(ctx context.Context, filter UserFilter) (*UserPage, error)

	// Auth 用户认证（用户名+密码）。
	//
	// 返回：认证成功返回用户对象，失败返回 ErrAuthFailed。
	// 内部自动记录登录失败次数，超过阈值锁定。
	Auth(ctx context.Context, username, password string) (*User, error)

	// AssignRoles 为用户分配角色列表。
	AssignRoles(ctx context.Context, userID string, roles []string) error

	// GetRoles 获取用户角色列表。
	GetRoles(ctx context.Context, userID string) ([]string, error)
}

// User 用户对象。
type User struct {
	ID          string     `json:"id"`           // 用户唯一标识
	Username    string     `json:"username"`     // 用户名（唯一）
	DisplayName string     `json:"displayName"`  // 显示名称
	Email       string     `json:"email"`        // 邮箱（唯一）
	Phone       string     `json:"phone"`        // 手机号
	TenantID    string     `json:"tenantId"`     // 所属租户 ID
	AvatarURL   string     `json:"avatarUrl"`    // 头像 URL
	Roles       []string   `json:"roles"`        // 角色列表
	Status      UserStatus `json:"status"`       // 用户状态
	LastLoginAt *time.Time `json:"lastLoginAt"`  // 最后登录时间
	LastLoginIP string     `json:"lastLoginIp"`  // 最后登录 IP
	CreatedAt   time.Time  `json:"createdAt"`    // 创建时间
	UpdatedAt   time.Time  `json:"updatedAt"`    // 更新时间
	DeletedAt   *time.Time `json:"deletedAt"`    // 删除时间（软删除）
}

// UserStatus 用户状态。
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"   // 正常
	UserStatusLocked   UserStatus = "locked"   // 已锁定
	UserStatusDisabled UserStatus = "disabled" // 已禁用
)

// CreateUserReq 创建用户请求。
type CreateUserReq struct {
	Username    string   `json:"username"`    // 用户名（必填，3-32 字符）
	Password    string   `json:"password"`    // 密码（必填，8-64 字符）
	DisplayName string   `json:"displayName"` // 显示名称
	Email       string   `json:"email"`       // 邮箱
	Phone       string   `json:"phone"`       // 手机号
	TenantID    string   `json:"tenantId"`    // 租户 ID
	Roles       []string `json:"roles"`       // 初始角色列表
}

// UpdateUserReq 更新用户请求。
type UpdateUserReq struct {
	DisplayName *string     `json:"displayName"` // 显示名称
	Email       *string     `json:"email"`       // 邮箱
	Phone       *string     `json:"phone"`       // 手机号
	AvatarURL   *string     `json:"avatarUrl"`   // 头像 URL
	Status      *UserStatus `json:"status"`      // 用户状态
}

// UserFilter 用户查询过滤条件。
type UserFilter struct {
	Keyword  string      `json:"keyword"`  // 搜索关键词（匹配用户名/邮箱/显示名）
	Status   *UserStatus `json:"status"`   // 状态过滤
	TenantID string      `json:"tenantId"` // 租户 ID 过滤
	Role     string      `json:"role"`     // 角色过滤
	Page     int         `json:"page"`     // 页码（1-based）
	PageSize int         `json:"pageSize"` // 每页条数
}

// UserPage 用户分页结果。
type UserPage struct {
	Items    []*User `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
	HasMore  bool    `json:"hasMore"`
}