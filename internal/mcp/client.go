package mcp

import "context"

// Client MCP 客户端接口。
//
// 职责：
//   - MCP 服务器注册/注销
//   - 工具发现（ListTools）
//   - 工具调用（CallTool）
//   - 多传输协议支持（stdio / HTTP / SSE）
type Client interface {
	// ListServers 列出已注册的 MCP 服务器。
	ListServers(ctx context.Context) ([]*Server, error)

	// Register 注册 MCP 服务器。
	Register(ctx context.Context, req RegisterServerReq) (*Server, error)

	// Unregister 注销 MCP 服务器。
	Unregister(ctx context.Context, serverID string) error

	// GetServer 获取服务器详情（含工具列表）。
	GetServer(ctx context.Context, serverID string) (*Server, error)

	// ListTools 列出指定服务器提供的工具列表。
	ListTools(ctx context.Context, serverID string) ([]*Tool, error)

	// CallTool 调用指定工具。
	CallTool(ctx context.Context, req CallToolReq) (*CallToolResp, error)

	// HealthCheck 检查 MCP 服务器连通性。
	HealthCheck(ctx context.Context) ([]*ServerHealth, error)

	// RefreshTools 刷新指定服务器的工具列表（重新发现）。
	RefreshTools(ctx context.Context, serverID string) error
}