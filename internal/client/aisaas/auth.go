package aisaas

import (
	"context"
	"fmt"
)

// ============================================================
// 设备鉴权
// ============================================================

// RegisterDevice 注册设备并获取 API Key。
//
// POST /internal/api/v1/devices/{deviceId}/register
// 鉴权: X-Internal-Token
func (c *Client) RegisterDevice(ctx context.Context, deviceID string, hwInfo *DeviceHWInfo) (*DeviceCredential, error) {
	if hwInfo == nil {
		hwInfo = &DeviceHWInfo{}
	}

	req := registerDeviceRequest{
		HwInfo: *hwInfo,
	}

	path := fmt.Sprintf("/internal/api/v1/devices/%s/register", deviceID)
	var resp registerDeviceResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "internal", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: register device %s: %w", deviceID, err)
	}

	cred := &DeviceCredential{
		DeviceID: resp.DeviceID,
		APIKey:   resp.APIKey,
		KeyID:    resp.KeyID,
	}
	if resp.ExpiresAt != "" {
		cred.ExpiresAt, _ = parseTime(resp.ExpiresAt)
	}

	return cred, nil
}

// VerifyKey 验证 API Key 是否有效。
//
// GET /v1/models
// 鉴权: Bearer API Key
// 返回 nil 表示 Key 有效；返回 error 表示 Key 无效或服务不可达。
func (c *Client) VerifyKey(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, "GET", "/v1/models", nil, "bearer", nil)
	if err != nil {
		return fmt.Errorf("aisaas: verify key: %w", err)
	}
	return nil
}

// Ping 验证 aisaas 服务连通性。
//
// HEAD /internal/api/v1/ping
// 鉴权: X-Internal-Token
func (c *Client) Ping(ctx context.Context) error {
	_, _, err := c.doRequest(ctx, "GET", "/internal/api/v1/ping", nil, "internal", nil)
	if err != nil {
		return fmt.Errorf("aisaas: ping: %w", err)
	}
	return nil
}
