package template

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"
)

// varPattern 匹配 {{.xxx}} 模板变量。
var varPattern = regexp.MustCompile(`\{\{\.(\w+)\}\}`)

// memoryTemplateManager 基于内存的模板管理实现。
type memoryTemplateManager struct {
	mu        sync.RWMutex
	templates map[string]*Template // name → Template
}

// NewManager 创建基于内存的模板管理器。
func NewManager() Manager {
	mgr := &memoryTemplateManager{
		templates: make(map[string]*Template),
	}
	mgr.initMockTemplates()
	return mgr
}

// initMockTemplates 初始化 5 个内置模板。
func (m *memoryTemplateManager) initMockTemplates() {
	now := time.Now()
	mock := []*Template{
		{
			Name:        "system_prompt",
			Content:     "你是{{.name}}，一个{{.personality}}的智能助手。你的职责是{{.role}}。请用{{.language}}回复用户。",
			Category:    "system_prompt",
			Variables:   []string{"name", "personality", "role", "language"},
			Description: "默认系统提示词模板",
			IsBuiltin:   true,
			CreatedBy:   "system",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "welcome",
			Content:     "你好{{.userName}}！我是{{.assistantName}}，很高兴为你服务。今天有什么可以帮你的？",
			Category:    "welcome",
			Variables:   []string{"userName", "assistantName"},
			Description: "默认欢迎语模板",
			IsBuiltin:   true,
			CreatedBy:   "system",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "fallback",
			Content:     "抱歉，我没有理解你的问题。你可以尝试换个方式描述，或者问我关于{{.topics}}的问题。",
			Category:    "fallback",
			Variables:   []string{"topics"},
			Description: "默认兜底回复模板",
			IsBuiltin:   true,
			CreatedBy:   "system",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "function_call",
			Content:     "我将调用工具 {{.toolName}} 来完成任务{{.task}}。请稍候...",
			Category:    "function_call",
			Variables:   []string{"toolName", "task"},
			Description: "默认工具调用模板",
			IsBuiltin:   true,
			CreatedBy:   "system",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "greeting",
			Content:     "{{.greeting}}！今天是{{.date}}，当前{{.weather}}。有什么需要我帮忙的吗？",
			Category:    "welcome",
			Variables:   []string{"greeting", "date", "weather"},
			Description: "场景化问候模板",
			IsBuiltin:   false,
			CreatedBy:   "system",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	for _, t := range mock {
		m.templates[t.Name] = t
	}
}

// Get 根据名称获取模板。
func (m *memoryTemplateManager) Get(ctx context.Context, name string) (*Template, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.templates[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	return t, nil
}

// List 列出所有模板（按分类过滤）。
func (m *memoryTemplateManager) List(ctx context.Context, filter TemplateFilter) ([]*Template, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []*Template
	for _, t := range m.templates {
		if filter.Category != "" && t.Category != filter.Category {
			continue
		}
		if filter.TenantID != "" && t.TenantID != filter.TenantID {
			continue
		}
		if filter.Keyword != "" {
			kw := strings.ToLower(filter.Keyword)
			if !strings.Contains(strings.ToLower(t.Name), kw) &&
				!strings.Contains(strings.ToLower(t.Description), kw) {
				continue
			}
		}
		matched = append(matched, t)
	}

	// 分页
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	start := (page - 1) * pageSize
	if start >= len(matched) {
		return []*Template{}, nil
	}
	end := start + pageSize
	if end > len(matched) {
		end = len(matched)
	}

	return matched[start:end], nil
}

// Create 创建新模板。
func (m *memoryTemplateManager) Create(ctx context.Context, req CreateTemplateReq) (*Template, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req.Name == "" {
		return nil, fmt.Errorf("template: name is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("template: content is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.templates[req.Name]; ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateExists, req.Name)
	}

	// 自动提取变量
	variables := req.Variables
	if len(variables) == 0 {
		variables = extractVariablesFromContent(req.Content)
	}

	now := time.Now()
	t := &Template{
		Name:        req.Name,
		Content:     req.Content,
		Category:    req.Category,
		Variables:   variables,
		Description: req.Description,
		TenantID:    req.TenantID,
		IsBuiltin:   false,
		CreatedBy:   "api",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.templates[req.Name] = t
	return t, nil
}

// Update 更新模板内容。
func (m *memoryTemplateManager) Update(ctx context.Context, name string, req UpdateTemplateReq) (*Template, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.templates[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}

	if req.Content != nil {
		t.Content = *req.Content
		// 自动重新提取变量
		if len(req.Variables) == 0 {
			t.Variables = extractVariablesFromContent(*req.Content)
		}
	}
	if req.Category != nil {
		t.Category = *req.Category
	}
	if len(req.Variables) > 0 {
		t.Variables = req.Variables
	}
	if req.Description != nil {
		t.Description = *req.Description
	}
	t.UpdatedAt = time.Now()

	return t, nil
}

// Delete 删除模板。
func (m *memoryTemplateManager) Delete(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.templates[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	if t.IsBuiltin {
		return fmt.Errorf("%w: %s", ErrTemplateBuiltin, name)
	}

	delete(m.templates, name)
	return nil
}

// Render 渲染模板。
func (m *memoryTemplateManager) Render(ctx context.Context, name string, vars map[string]any) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	m.mu.RLock()
	t, ok := m.templates[name]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}

	return renderTemplate(t.Content, vars)
}

// Preview 预览模板渲染结果。
func (m *memoryTemplateManager) Preview(ctx context.Context, content string, vars map[string]any) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if content == "" {
		return "", fmt.Errorf("template: content is empty")
	}
	return renderTemplate(content, vars)
}

// ExtractVariables 提取模板中使用的变量列表。
func (m *memoryTemplateManager) ExtractVariables(ctx context.Context, content string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if content == "" {
		return nil, fmt.Errorf("template: content is empty")
	}
	return extractVariablesFromContent(content), nil
}

// Copy 复制模板。
func (m *memoryTemplateManager) Copy(ctx context.Context, sourceName, targetName string) (*Template, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	src, ok := m.templates[sourceName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateNotFound, sourceName)
	}

	if _, ok := m.templates[targetName]; ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateExists, targetName)
	}

	now := time.Now()
	target := &Template{
		Name:        targetName,
		Content:     src.Content,
		Category:    src.Category,
		Variables:   append([]string{}, src.Variables...),
		Description: fmt.Sprintf("Copy of %s", src.Description),
		TenantID:    src.TenantID,
		IsBuiltin:   false,
		CreatedBy:   "api",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.templates[targetName] = target
	return target, nil
}

// renderTemplate 执行 Go text/template 渲染。
func renderTemplate(content string, vars map[string]any) (string, error) {
	tmpl, err := template.New("inline").Parse(content)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRenderFailed, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRenderFailed, err)
	}

	return buf.String(), nil
}

// extractVariablesFromContent 从模板内容中提取 {{.xxx}} 变量。
func extractVariablesFromContent(content string) []string {
	matches := varPattern.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var vars []string
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			vars = append(vars, m[1])
		}
	}
	return vars
}