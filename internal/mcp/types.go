// Package mcp MCP（Model Context Protocol）客户端。
//
// 职责：管理外部工具服务器的注册与调用。
// 实现：本地管理 + 委托 aisaas.Client.CallMCP() 执行工具调用。
package mcp

import (
	"errors"
	"time"
)

// Server MCP 服务器信息。
type Server struct {
	ID           string            `json:"id"`           // 服务器唯一标识
	Name         string            `json:"name"`         // 服务器名称
	Transport    string            `json:"transport"`    // 传输协议：stdio / http / sse
	Command      string            `json:"command"`      // 启动命令（stdio 模式）
	Args         []string          `json:"args"`         // 命令参数（stdio 模式）
	URL          string            `json:"url"`          // 服务地址（http/sse 模式）
	EnvVars      map[string]string `json:"envVars"`      // 环境变量（stdio 模式）
	Description  string            `json:"description"`  // 服务器描述
	Tools        []*Tool           `json:"tools"`        // 提供的工具列表
	Status       string            `json:"status"`       // 状态：connected / disconnected / error
	RegisteredAt time.Time         `json:"registeredAt"` // 注册时间
	LastSeenAt   time.Time         `json:"lastSeenAt"`   // 最后在线时间
}

// RegisterServerReq 注册 MCP 服务器请求。
type RegisterServerReq struct {
	Name        string            `json:"name"`        // 服务器名称（必填）
	Transport   string            `json:"transport"`   // 传输协议（必填）
	Command     string            `json:"command"`     // 启动命令（stdio）
	Args        []string          `json:"args"`        // 命令参数
	URL         string            `json:"url"`         // 服务地址（http/sse）
	EnvVars     map[string]string `json:"envVars"`     // 环境变量
	Description string            `json:"description"` // 描述
}

// Tool MCP 工具定义。
type Tool struct {
	Name        string         `json:"name"`        // 工具名称
	Description string         `json:"description"` // 工具描述
	InputSchema map[string]any `json:"inputSchema"` // 输入参数 JSON Schema
}

// CallToolReq 工具调用请求。
type CallToolReq struct {
	ServerID  string         `json:"serverId"`  // 服务器 ID
	ToolName  string         `json:"toolName"`  // 工具名称
	Arguments map[string]any `json:"arguments"` // 调用参数
	Timeout   time.Duration  `json:"timeout"`   // 超时时间（默认 30s）
}

// CallToolResp 工具调用响应。
type CallToolResp struct {
	Content   []ToolContent `json:"content"`   // 返回内容
	IsError   bool          `json:"isError"`   // 是否为错误
	ToolName  string        `json:"toolName"`  // 工具名称
	LatencyMs int64         `json:"latencyMs"` // 调用延迟（毫秒）
}

// ToolContent 工具返回内容项。
type ToolContent struct {
	Type     string `json:"type"`     // 类型：text / image / resource
	Text     string `json:"text"`     // 文本内容（type=text）
	Data     string `json:"data"`     // 数据（base64，type=image）
	MimeType string `json:"mimeType"` // MIME 类型（type=image）
}

// ServerHealth MCP 服务器健康状态。
type ServerHealth struct {
	ServerID   string    `json:"serverId"`   // 服务器 ID
	ServerName string    `json:"serverName"` // 服务器名称
	Status     string    `json:"status"`     // 状态：connected / disconnected / error
	LatencyMs  int64     `json:"latencyMs"`  // 延迟（毫秒）
	Error      string    `json:"error"`      // 错误信息（健康时为 empty）
	CheckedAt  time.Time `json:"checkedAt"`  // 检查时间
}

// 哨兵错误。
var (
	ErrMCPServerNotFound  = errors.New("mcp: server not found")
	ErrMCPServerExists    = errors.New("mcp: server already registered")
	ErrMCPServerUnhealthy = errors.New("mcp: server is not healthy")
	ErrMCPToolNotFound    = errors.New("mcp: tool not found")
	ErrMCPTimeout         = errors.New("mcp: tool call timeout")
)