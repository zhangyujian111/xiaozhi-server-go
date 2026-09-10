package aisaas

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ============================================================
// Memory 模块（5 个接口）
// ============================================================

// WriteMessage 写入短期会话消息。
//
// POST /api/v1/memories/{deviceId}/messages
// 鉴权: Bearer API Key
func (c *Client) WriteMessage(ctx context.Context, deviceID string, msg *WriteMemoryMessageRequest) (*WriteMemoryMessageResponse, error) {
	if msg == nil {
		return nil, fmt.Errorf("aisaas: write message request is nil")
	}
	if msg.Role == "" {
		return nil, fmt.Errorf("aisaas: write message role is required")
	}
	if msg.Content == "" {
		return nil, fmt.Errorf("aisaas: write message content is required")
	}

	path := fmt.Sprintf("/api/v1/memories/%s/messages", deviceID)
	var resp WriteMemoryMessageResponse
	_, _, err := c.doRequest(ctx, "POST", path, msg, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: write message %s: %w", deviceID, err)
	}

	return &resp, nil
}

// ListMessages 列出短期会话消息。
//
// GET /api/v1/memories/{deviceId}/messages?limit=50&cursor=xxx
// 鉴权: Bearer API Key
func (c *Client) ListMessages(ctx context.Context, deviceID string, limit int, cursor string) (*ListMemoryMessagesResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	path := fmt.Sprintf("/api/v1/memories/%s/messages?%s", deviceID, params.Encode())
	var resp ListMemoryMessagesResponse
	_, _, err := c.doRequest(ctx, "GET", path, nil, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: list messages %s: %w", deviceID, err)
	}

	return &resp, nil
}

// GetMemoryGraph 拉取长期记忆图谱。
//
// GET /api/v1/memories/{deviceId}?limit=20&dimension=entity
// 鉴权: Bearer API Key
func (c *Client) GetMemoryGraph(ctx context.Context, deviceID string, limit int, dimension string) (*MemoryGraphResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if dimension != "" {
		params.Set("dimension", dimension)
	}

	path := fmt.Sprintf("/api/v1/memories/%s?%s", deviceID, params.Encode())
	var resp MemoryGraphResponse
	_, _, err := c.doRequest(ctx, "GET", path, nil, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: get memory graph %s: %w", deviceID, err)
	}

	return &resp, nil
}

// ExtractMemory 异步抽取实体。
//
// POST /api/v1/memories/{deviceId}/extract
// 鉴权: Bearer API Key
func (c *Client) ExtractMemory(ctx context.Context, deviceID string, req *ExtractMemoryRequest) (*ExtractMemoryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("aisaas: extract memory request is nil")
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("aisaas: extract memory messages is empty")
	}
	if len(req.ExtractTypes) == 0 {
		return nil, fmt.Errorf("aisaas: extract memory types is empty")
	}

	path := fmt.Sprintf("/api/v1/memories/%s/extract", deviceID)
	var resp ExtractMemoryResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: extract memory %s: %w", deviceID, err)
	}

	return &resp, nil
}

// SummarizeMemory 异步摘要聚合。
//
// POST /api/v1/memories/{deviceId}/summarize
// 鉴权: Bearer API Key
func (c *Client) SummarizeMemory(ctx context.Context, deviceID string, req *SummarizeMemoryRequest) (*SummarizeMemoryResponse, error) {
	if req == nil {
		req = &SummarizeMemoryRequest{
			SummarizeTypes: []string{"conversation", "topic"},
		}
	}
	if len(req.SummarizeTypes) == 0 {
		req.SummarizeTypes = []string{"conversation", "topic"}
	}

	path := fmt.Sprintf("/api/v1/memories/%s/summarize", deviceID)
	var resp SummarizeMemoryResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: summarize memory %s: %w", deviceID, err)
	}

	return &resp, nil
}
