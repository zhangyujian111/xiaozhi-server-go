// Package template 提示词模板管理服务。
//
// 职责：提示词模板的 CRUD 与渲染（Go text/template 引擎）。
// 实现：内存 mock 5 个 Go text/template。
package template

import (
	"errors"
	"time"
)

// Template 模板对象。
type Template struct {
	Name        string    `json:"name"`        // 模板名称（唯一标识）
	Content     string    `json:"content"`     // 模板内容（Go template 语法）
	Category    string    `json:"category"`    // 分类：system_prompt / welcome / fallback / function_call
	Variables   []string  `json:"variables"`   // 声明的变量列表
	Description string    `json:"description"` // 模板描述
	TenantID    string    `json:"tenantId"`    // 租户 ID
	IsBuiltin   bool      `json:"isBuiltin"`   // 是否为内置模板（不可删除）
	CreatedBy   string    `json:"createdBy"`   // 创建者
	CreatedAt   time.Time `json:"createdAt"`   // 创建时间
	UpdatedAt   time.Time `json:"updatedAt"`   // 更新时间
}

// CreateTemplateReq 创建模板请求。
type CreateTemplateReq struct {
	Name        string   `json:"name"`        // 模板名称（必填）
	Content     string   `json:"content"`     // 模板内容（必填）
	Category    string   `json:"category"`    // 分类（必填）
	Variables   []string `json:"variables"`   // 变量列表
	Description string   `json:"description"` // 描述
	TenantID    string   `json:"tenantId"`    // 租户 ID
}

// UpdateTemplateReq 更新模板请求。
type UpdateTemplateReq struct {
	Content     *string  `json:"content"`     // 模板内容
	Category    *string  `json:"category"`    // 分类
	Variables   []string `json:"variables"`   // 变量列表
	Description *string  `json:"description"` // 描述
}

// TemplateFilter 模板查询过滤条件。
type TemplateFilter struct {
	Category string `json:"category"` // 分类过滤
	TenantID string `json:"tenantId"` // 租户过滤
	Keyword  string `json:"keyword"`  // 名称/描述搜索
	Page     int    `json:"page"`     // 页码
	PageSize int    `json:"pageSize"` // 每页条数
}

// 哨兵错误。
var (
	ErrTemplateNotFound = errors.New("template: template not found")
	ErrTemplateExists   = errors.New("template: template already exists")
	ErrMissingVariable  = errors.New("template: required variable not provided")
	ErrRenderFailed     = errors.New("template: render failed")
	ErrTemplateBuiltin  = errors.New("template: builtin template cannot be deleted")
)