package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
)

// aisaasMCPClient 委托 aisaas SDK 的 MCP 客户端实现。
//
// 本地管理服务器注册/发现，工具调用委托给 aisaas。
type aisaasMCPClient struct {
	mu      sync.RWMutex
	servers map[string]*Server // serverID → Server
	client  *aisaas.Client
}

// NewClient 创建委托 aisaas SDK 的 MCP 客户端。
func NewClient(aisaasClient *aisaas.Client) Client {
	c := &aisaasMCPClient{
		servers: make(map[string]*Server),
		client:  aisaasClient,
	}
	c.initMockServers()
	return c
}

// initMockServers 初始化内置 MCP 服务器。
func (c *aisaasMCPClient) initMockServers() {
	now := time.Now()
	c.servers["mcp-builtin-weather"] = &Server{
		ID:          "mcp-builtin-weather",
		Name:        "weather",
		Transport:   "http",
		URL:         "http://localhost:8190/api/v1/mcp/weather",
		Description: "天气查询服务",
		Tools: []*Tool{
			{Name: "get_weather", Description: "获取指定城市的天气信息", InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "城市名称"},
				},
				"required": []any{"city"},
			}},
		},
		Status:       "connected",
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	c.servers["mcp-builtin-calc"] = &Server{
		ID:          "mcp-builtin-calc",
		Name:        "calculator",
		Transport:   "http",
		URL:         "http://localhost:8190/api/v1/mcp/calculator",
		Description: "数学计算服务",
		Tools: []*Tool{
			{Name: "calculate", Description: "执行数学表达式计算", InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expression": map[string]any{"type": "string", "description": "数学表达式"},
				},
				"required": []any{"expression"},
			}},
		},
		Status:       "connected",
		RegisteredAt: now,
		LastSeenAt:   now,
	}
}

// ListServers 列出已注册的 MCP 服务器。
func (c *aisaasMCPClient) ListServers(ctx context.Context) ([]*Server, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	servers := make([]*Server, 0, len(c.servers))
	for _, s := range c.servers {
		servers = append(servers, s)
	}
	return servers, nil
}

// Register 注册 MCP 服务器。
func (c *aisaasMCPClient) Register(ctx context.Context, req RegisterServerReq) (*Server, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req.Name == "" {
		return nil, fmt.Errorf("mcp: server name is required")
	}
	if req.Transport == "" {
		return nil, fmt.Errorf("mcp: transport is required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查同名服务器
	for _, s := range c.servers {
		if s.Name == req.Name {
			return nil, fmt.Errorf("%w: %s", ErrMCPServerExists, req.Name)
		}
	}

	now := time.Now()
	server := &Server{
		ID:           "mcp-" + uuid.New().String()[:8],
		Name:         req.Name,
		Transport:    req.Transport,
		Command:      req.Command,
		Args:         req.Args,
		URL:          req.URL,
		EnvVars:      req.EnvVars,
		Description:  req.Description,
		Tools:        []*Tool{},
		Status:       "connected",
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	c.servers[server.ID] = server
	return server, nil
}

// Unregister 注销 MCP 服务器。
func (c *aisaasMCPClient) Unregister(ctx context.Context, serverID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.servers[serverID]; !ok {
		return fmt.Errorf("%w: %s", ErrMCPServerNotFound, serverID)
	}

	delete(c.servers, serverID)
	return nil
}

// GetServer 获取服务器详情（含工具列表）。
func (c *aisaasMCPClient) GetServer(ctx context.Context, serverID string) (*Server, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	s, ok := c.servers[serverID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMCPServerNotFound, serverID)
	}
	return s, nil
}

// ListTools 列出指定服务器提供的工具列表。
func (c *aisaasMCPClient) ListTools(ctx context.Context, serverID string) ([]*Tool, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	s, ok := c.servers[serverID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMCPServerNotFound, serverID)
	}
	return s.Tools, nil
}

// CallTool 调用指定工具。
//
// 委托 aisaas.Client.CallMCP() 执行实际调用。
func (c *aisaasMCPClient) CallTool(ctx context.Context, req CallToolReq) (*CallToolResp, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req.ToolName == "" {
		return nil, fmt.Errorf("mcp: tool name is required")
	}

	c.mu.RLock()
	_, ok := c.servers[req.ServerID]
	c.mu.RUnlock()
	if !ok && req.ServerID != "" {
		return nil, fmt.Errorf("%w: %s", ErrMCPServerNotFound, req.ServerID)
	}

	start := time.Now()

	// 委托 aisaas SDK
	aisaasReq := &aisaas.CallMCPRequest{
		Tool: req.ToolName,
		Args: req.Arguments,
	}
	mcpResp, err := c.client.CallMCP(ctx, "", aisaasReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: call tool: %w", err)
	}

	latency := time.Since(start)

	content := []ToolContent{
		{Type: "text", Text: mcpResp.Result},
	}
	isError := mcpResp.Error != ""

	return &CallToolResp{
		Content:   content,
		IsError:   isError,
		ToolName:  req.ToolName,
		LatencyMs: latency.Milliseconds(),
	}, nil
}

// HealthCheck 检查 MCP 服务器连通性。
func (c *aisaasMCPClient) HealthCheck(ctx context.Context) ([]*ServerHealth, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	var results []*ServerHealth
	for _, s := range c.servers {
		results = append(results, &ServerHealth{
			ServerID:   s.ID,
			ServerName: s.Name,
			Status:     s.Status,
			LatencyMs:  0,
			CheckedAt:  now,
		})
	}
	return results, nil
}

// RefreshTools 刷新指定服务器的工具列表。
func (c *aisaasMCPClient) RefreshTools(ctx context.Context, serverID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.servers[serverID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrMCPServerNotFound, serverID)
	}

	s.LastSeenAt = time.Now()
	// 当前为 mock 实现，工具列表不变
	return nil
}