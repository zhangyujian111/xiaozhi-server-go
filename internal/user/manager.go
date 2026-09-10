package user

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost        = 12              // BCrypt 加密成本
	maxLoginFailures  = 5               // 最大登录失败次数
	lockoutDuration   = 15 * time.Minute // 锁定时间
)

// managerImpl 内存用户管理器实现。
//
// 线程安全：所有公共方法受 mu 保护。
type managerImpl struct {
	mu             sync.RWMutex
	users          map[string]*User       // userID → User
	usernameIdx    map[string]string      // username → userID
	emailIdx       map[string]string      // email → userID
	passwordHashes map[string]string      // userID → BCrypt hash
	loginFails     map[string]int         // userID → 失败次数
	lockedUntil    map[string]time.Time   // userID → 锁定到期时间
	logger         *slog.Logger
}

// NewManager 创建内存用户管理器。
//
// 预加载 5 个测试用户（BCrypt 密码哈希）。
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
		users:          make(map[string]*User),
		usernameIdx:    make(map[string]string),
		emailIdx:       make(map[string]string),
		passwordHashes: make(map[string]string),
		loginFails:     make(map[string]int),
		lockedUntil:    make(map[string]time.Time),
		logger:         logger.With("component", "user.manager"),
	}

	// 预加载 5 个测试用户（密码均为 "test123"）
	m.seedTestUsers()

	return m
}

// seedTestUsers 预加载测试用户。
func (m *managerImpl) seedTestUsers() {
	testUsers := []struct {
		username    string
		displayName string
		email       string
		roles       []string
	}{
		{"admin", "Administrator", "admin@xiaozhi.local", []string{"admin", "user"}},
		{"user1", "User One", "user1@xiaozhi.local", []string{"user"}},
		{"user2", "User Two", "user2@xiaozhi.local", []string{"user"}},
		{"test", "Test User", "test@xiaozhi.local", []string{"user", "tester"}},
		{"demo", "Demo User", "demo@xiaozhi.local", []string{"user"}},
	}

	// 统一密码
	password := "test123"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		// 种子用户创建失败不应阻止启动，记录错误即可
		m.logger.Error("failed to hash seed user password", "error", err)
		return
	}

	for _, tu := range testUsers {
		id, _ := uuid.NewV7()
		now := time.Now()

		user := &User{
			ID:          id.String(),
			Username:    tu.username,
			DisplayName: tu.displayName,
			Email:       tu.email,
			Phone:       "",
			TenantID:    "default",
			AvatarURL:   "",
			Roles:       tu.roles,
			Status:      UserStatusActive,
			LastLoginAt: nil,
			LastLoginIP: "",
			CreatedAt:   now,
			UpdatedAt:   now,
			DeletedAt:   nil,
		}

		m.users[user.ID] = user
		m.usernameIdx[tu.username] = user.ID
		m.emailIdx[tu.email] = user.ID

		m.passwordHashes[user.ID] = string(hashedPassword)
	}

	m.logger.Info("seed test users created", "count", len(testUsers))
}

// Create 创建新用户。
func (m *managerImpl) Create(ctx context.Context, req CreateUserReq) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 验证用户名长度
	if len(req.Username) < 3 || len(req.Username) > 32 {
		return nil, fmt.Errorf("user: username must be 3-32 characters")
	}

	// 验证密码长度
	if len(req.Password) < 8 || len(req.Password) > 64 {
		return nil, ErrWeakPassword
	}

	// 检查用户名唯一性
	if _, exists := m.usernameIdx[req.Username]; exists {
		return nil, ErrUserExists
	}

	// 检查邮箱唯一性
	if req.Email != "" {
		if _, exists := m.emailIdx[req.Email]; exists {
			return nil, ErrUserExists
		}
	}

	// BCrypt 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("user: hash password: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("user: generate uuid: %w", err)
	}

	now := time.Now()
	user := &User{
		ID:          id.String(),
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Phone:       req.Phone,
		TenantID:    req.TenantID,
		AvatarURL:   "",
		Roles:       req.Roles,
		Status:      UserStatusActive,
		LastLoginAt: nil,
		LastLoginIP: "",
		CreatedAt:   now,
		UpdatedAt:   now,
		DeletedAt:   nil,
	}

	if user.Roles == nil {
		user.Roles = []string{}
	}

	m.users[user.ID] = user
	m.usernameIdx[user.Username] = user.ID
	if user.Email != "" {
		m.emailIdx[user.Email] = user.ID
	}

	// 存储密码哈希
	m.passwordHashes[user.ID] = string(hashedPassword)

	m.logger.InfoContext(ctx, "user created",
		"userId", user.ID,
		"username", user.Username,
	)

	return copyUser(user), nil
}

// Get 根据用户 ID 查询用户。
func (m *managerImpl) Get(ctx context.Context, userID string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return nil, ErrUserNotFound
	}

	return copyUser(user), nil
}

// GetByUsername 根据用户名查询用户。
func (m *managerImpl) GetByUsername(ctx context.Context, username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userID, ok := m.usernameIdx[username]
	if !ok {
		return nil, ErrUserNotFound
	}

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return nil, ErrUserNotFound
	}

	return copyUser(user), nil
}

// Update 更新用户信息（仅更新非零值字段）。
func (m *managerImpl) Update(ctx context.Context, userID string, req UpdateUserReq) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return nil, ErrUserNotFound
	}

	// 更新邮箱时检查唯一性
	if req.Email != nil && *req.Email != user.Email {
		if _, exists := m.emailIdx[*req.Email]; exists {
			return nil, ErrUserExists
		}
		// 移除旧邮箱索引
		if user.Email != "" {
			delete(m.emailIdx, user.Email)
		}
		user.Email = *req.Email
		m.emailIdx[user.Email] = user.ID
	}

	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if req.Phone != nil {
		user.Phone = *req.Phone
	}
	if req.AvatarURL != nil {
		user.AvatarURL = *req.AvatarURL
	}
	if req.Status != nil {
		user.Status = *req.Status
	}

	user.UpdatedAt = time.Now()

	m.logger.InfoContext(ctx, "user updated",
		"userId", user.ID,
		"username", user.Username,
	)

	return copyUser(user), nil
}

// UpdatePassword 更新用户密码（需验证旧密码）。
func (m *managerImpl) UpdatePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return ErrUserNotFound
	}

	// 验证新密码强度
	if len(newPassword) < 8 || len(newPassword) > 64 {
		return ErrWeakPassword
	}

	// 验证旧密码
	hashedPassword, ok := m.passwordHashes[userID]
	if !ok {
		return ErrAuthFailed
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(oldPassword)); err != nil {
		return ErrAuthFailed
	}

	// BCrypt 加密新密码
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("user: hash password: %w", err)
	}

	m.passwordHashes[userID] = string(newHash)
	user.UpdatedAt = time.Now()

	m.logger.InfoContext(ctx, "user password updated",
		"userId", user.ID,
		"username", user.Username,
	)

	return nil
}

// Delete 软删除用户（设置 deleted_at）。
func (m *managerImpl) Delete(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return ErrUserNotFound
	}

	now := time.Now()
	user.DeletedAt = &now
	user.UpdatedAt = now

	// 清理索引（软删除用户不可通过索引查询）
	delete(m.usernameIdx, user.Username)
	if user.Email != "" {
		delete(m.emailIdx, user.Email)
	}

	m.logger.InfoContext(ctx, "user deleted (soft)",
		"userId", user.ID,
		"username", user.Username,
	)

	return nil
}

// List 分页查询用户列表。
func (m *managerImpl) List(ctx context.Context, filter UserFilter) (*UserPage, error) {
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

	// 收集匹配的用户
	var matched []*User
	for _, user := range m.users {
		// 跳过已删除的用户
		if user.DeletedAt != nil {
			continue
		}

		// 关键词过滤
		if filter.Keyword != "" {
			kw := strings.ToLower(filter.Keyword)
			if !strings.Contains(strings.ToLower(user.Username), kw) &&
				!strings.Contains(strings.ToLower(user.Email), kw) &&
				!strings.Contains(strings.ToLower(user.DisplayName), kw) {
				continue
			}
		}

		// 状态过滤
		if filter.Status != nil && user.Status != *filter.Status {
			continue
		}

		// 租户过滤
		if filter.TenantID != "" && user.TenantID != filter.TenantID {
			continue
		}

		// 角色过滤
		if filter.Role != "" {
			hasRole := false
			for _, r := range user.Roles {
				if r == filter.Role {
					hasRole = true
					break
				}
			}
			if !hasRole {
				continue
			}
		}

		matched = append(matched, copyUser(user))
	}

	total := int64(len(matched))

	// 分页切片
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(matched) {
		return &UserPage{
			Items:    []*User{},
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

	return &UserPage{
		Items:    matched[start:end],
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		HasMore:  end < len(matched),
	}, nil
}

// Auth 用户认证（用户名+密码）。
func (m *managerImpl) Auth(ctx context.Context, username, password string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 查找用户
	userID, ok := m.usernameIdx[username]
	if !ok {
		// 用户不存在，返回通用认证失败（防止用户枚举）
		return nil, ErrAuthFailed
	}

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return nil, ErrAuthFailed
	}

	// 检查是否被锁定
	if lockedUntil, ok := m.lockedUntil[userID]; ok {
		if time.Now().Before(lockedUntil) {
			return nil, ErrUserLocked
		}
		// 锁定已过期，清除记录
		delete(m.lockedUntil, userID)
		delete(m.loginFails, userID)
	}

	// 验证密码
	hashedPassword, ok := m.passwordHashes[userID]
	if !ok {
		return nil, ErrAuthFailed
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)); err != nil {
		// 登录失败，累计失败次数
		m.loginFails[userID]++
		if m.loginFails[userID] >= maxLoginFailures {
			m.lockedUntil[userID] = time.Now().Add(lockoutDuration)
			user.Status = UserStatusLocked
			m.logger.WarnContext(ctx, "user account locked",
				"userId", user.ID,
				"username", user.Username,
				"failures", m.loginFails[userID],
			)
			return nil, ErrUserLocked
		}

		m.logger.WarnContext(ctx, "user auth failed",
			"username", username,
			"failures", m.loginFails[userID],
		)
		return nil, ErrAuthFailed
	}

	// 登录成功，清除失败记录
	delete(m.loginFails, userID)
	delete(m.lockedUntil, userID)

	// 更新登录信息
	now := time.Now()
	user.LastLoginAt = &now
	user.LastLoginIP = "" // 由上层 HTTP handler 注入
	user.UpdatedAt = now

	m.logger.InfoContext(ctx, "user authenticated",
		"userId", user.ID,
		"username", user.Username,
	)

	return copyUser(user), nil
}

// AssignRoles 为用户分配角色列表。
func (m *managerImpl) AssignRoles(ctx context.Context, userID string, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return ErrUserNotFound
	}

	user.Roles = roles
	if user.Roles == nil {
		user.Roles = []string{}
	}
	user.UpdatedAt = time.Now()

	m.logger.InfoContext(ctx, "user roles assigned",
		"userId", user.ID,
		"username", user.Username,
		"roles", roles,
	)

	return nil
}

// GetRoles 获取用户角色列表。
func (m *managerImpl) GetRoles(ctx context.Context, userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, ok := m.users[userID]
	if !ok || user.DeletedAt != nil {
		return nil, ErrUserNotFound
	}

	roles := make([]string, len(user.Roles))
	copy(roles, user.Roles)
	return roles, nil
}

// copyUser 深拷贝用户对象（避免外部修改内部状态）。
func copyUser(u *User) *User {
	cp := *u
	if u.Roles != nil {
		cp.Roles = make([]string, len(u.Roles))
		copy(cp.Roles, u.Roles)
	}
	if u.LastLoginAt != nil {
		t := *u.LastLoginAt
		cp.LastLoginAt = &t
	}
	if u.DeletedAt != nil {
		t := *u.DeletedAt
		cp.DeletedAt = &t
	}
	return &cp
}