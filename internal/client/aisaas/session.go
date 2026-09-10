package aisaas

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ============================================================
// Session 模块（3 个接口）
// ============================================================

// CreateSession 创建会话并借记配额。
//
// POST /api/v1/sessions/{deviceId}
// 鉴权: Bearer API Key
//
// dimension 指定借记维度（如 llm_tokens_in），quotaInitial 为申请借记量。
// 返回会话信息含配额快照。
func (c *Client) CreateSession(ctx context.Context, deviceID string, dimension string, quotaInitial int64, personaID *int64) (*CreateSessionResponse, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("aisaas: device id is required")
	}
	if dimension == "" {
		return nil, fmt.Errorf("aisaas: dimension is required")
	}
	if quotaInitial <= 0 {
		return nil, fmt.Errorf("aisaas: quota initial must be > 0")
	}

	req := CreateSessionRequest{
		DeviceID:     deviceID,
		Dimension:    dimension,
		QuotaInitial: quotaInitial,
		PersonaID:    personaID,
	}

	path := fmt.Sprintf("/api/v1/sessions/%s", deviceID)
	var resp CreateSessionResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: create session %s: %w", deviceID, err)
	}

	return &resp, nil
}

// EndSession 结算会话实际消耗（含配额退款）。
//
// POST /api/v1/sessions/{sessionId}/end
// 鉴权: Bearer API Key
//
// actualCost 为实际消耗量，status 为会话结束状态（success/failed）。
func (c *Client) EndSession(ctx context.Context, sessionID string, actualCost QuotaUsage, status string) (*EndSessionResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("aisaas: session id is required")
	}
	if status == "" {
		status = "success"
	}

	req := EndSessionRequest{
		ActualCost: actualCost,
		Status:     status,
	}

	path := fmt.Sprintf("/api/v1/sessions/%s/end", sessionID)
	var resp EndSessionResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: end session %s: %w", sessionID, err)
	}

	return &resp, nil
}

// GetSessionHistory 拉取会话历史。
//
// GET /api/v1/sessions/{deviceId}/history?limit=10&cursor=xxx
// 鉴权: Bearer API Key
func (c *Client) GetSessionHistory(ctx context.Context, deviceID string, limit int, cursor string) (*SessionHistoryResponse, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("aisaas: device id is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	path := fmt.Sprintf("/api/v1/sessions/%s/history?%s", deviceID, params.Encode())
	var resp SessionHistoryResponse
	_, _, err := c.doRequest(ctx, "GET", path, nil, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: get session history %s: %w", deviceID, err)
	}

	return &resp, nil
}
