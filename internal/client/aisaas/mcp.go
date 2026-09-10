package aisaas

import (
	"context"
	"fmt"
)

// ============================================================
// MCP 工具调用
// ============================================================

// CallMCPRequest MCP 工具调用请求。
type CallMCPRequest struct {
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args"`
	DeviceID string         `json:"deviceId,omitempty"`
}

// CallMCPResponse MCP 工具调用响应。
type CallMCPResponse struct {
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// CallMCP 调用 MCP 工具。
//
// POST /api/v1/mcp/{deviceId}/call
// 鉴权: Bearer API Key
//
// 将设备侧的 MCP 工具调用请求转发到 aisaas MCP 服务，
// 返回工具执行结果。
func (c *Client) CallMCP(ctx context.Context, deviceID string, req *CallMCPRequest) (*CallMCPResponse, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("aisaas: device id is required")
	}
	if req == nil {
		return nil, fmt.Errorf("aisaas: call mcp request is nil")
	}
	if req.Tool == "" {
		return nil, fmt.Errorf("aisaas: mcp tool name is required")
	}

	// 确保 deviceID 在请求体中也一致
	req.DeviceID = deviceID

	path := fmt.Sprintf("/api/v1/mcp/%s/call", deviceID)
	var resp CallMCPResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: call mcp %s: %w", deviceID, err)
	}

	return &resp, nil
}
