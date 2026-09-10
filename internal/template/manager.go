package template

import "context"

// Manager 模板管理接口。
//
// 职责：
//   - 模板 CRUD
//   - 模板渲染（Go text/template 引擎）
//   - 模板变量提取与校验
type Manager interface {
	// Get 根据名称获取模板。
	Get(ctx context.Context, name string) (*Template, error)

	// List 列出所有模板（按分类过滤）。
	List(ctx context.Context, filter TemplateFilter) ([]*Template, error)

	// Create 创建新模板。
	Create(ctx context.Context, req CreateTemplateReq) (*Template, error)

	// Update 更新模板内容。
	Update(ctx context.Context, name string, req UpdateTemplateReq) (*Template, error)

	// Delete 删除模板。
	Delete(ctx context.Context, name string) error

	// Render 渲染模板。
	Render(ctx context.Context, name string, vars map[string]any) (string, error)

	// Preview 预览模板渲染结果（不实际保存，仅用于编辑时预览）。
	Preview(ctx context.Context, content string, vars map[string]any) (string, error)

	// ExtractVariables 提取模板中使用的变量列表。
	ExtractVariables(ctx context.Context, content string) ([]string, error)

	// Copy 复制模板（用于创建变体）。
	Copy(ctx context.Context, sourceName, targetName string) (*Template, error)
}