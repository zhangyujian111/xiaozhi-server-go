package aisaas

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ============================================================
// Persona 模块（2 个接口）
// ============================================================

// GetPersona 按 ID 读取人设详情。
//
// GET /api/v1/personas/{id}
// 鉴权: Bearer API Key
func (c *Client) GetPersona(ctx context.Context, personaID int64) (*Persona, error) {
	if personaID <= 0 {
		return nil, fmt.Errorf("aisaas: persona id is required")
	}

	path := fmt.Sprintf("/api/v1/personas/%d", personaID)
	var resp Persona
	_, _, err := c.doRequest(ctx, "GET", path, nil, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: get persona %d: %w", personaID, err)
	}

	return &resp, nil
}

// ListPersonas 按设备查询 Persona 列表（含绑定关系）。
//
// GET /api/v1/personas?deviceId=xxx&limit=10
// 鉴权: Bearer API Key
func (c *Client) ListPersonas(ctx context.Context, deviceID string, limit int) (*ListPersonasResponse, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("aisaas: device id is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	params := url.Values{}
	params.Set("deviceId", deviceID)
	params.Set("limit", strconv.Itoa(limit))

	path := fmt.Sprintf("/api/v1/personas?%s", params.Encode())
	var resp ListPersonasResponse
	_, _, err := c.doRequest(ctx, "GET", path, nil, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: list personas %s: %w", deviceID, err)
	}

	return &resp, nil
}
