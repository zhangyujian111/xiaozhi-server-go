package aisaas

import (
	"context"
	"fmt"
	"time"
)

// ============================================================
// API Key 生命周期管理
// ============================================================

// RotateKey 轮换 API Key。
//
// POST /internal/api/v1/apikeys/{keyId}/rotate
// 鉴权: X-Internal-Token
//
// 旧 Key 保留 5 分钟宽限期后自动失效，新 Key 明文仅此一次返回。
// 调用后自动更新 Client 内部 apiKey。
func (c *Client) RotateKey(ctx context.Context, keyID int64, expireDays int) (*DeviceCredential, error) {
	if expireDays <= 0 {
		expireDays = 1
	}

	req := RotateKeyRequest{
		ExpireDays:     expireDays,
		RotateStrategy: "time_24h",
	}

	path := fmt.Sprintf("/internal/api/v1/apikeys/%d/rotate", keyID)
	var resp RotateKeyResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "internal", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: rotate key %d: %w", keyID, err)
	}

	// 更新内部 apiKey
	c.apiKey = resp.NewAPIKey

	cred := &DeviceCredential{
		APIKey: resp.NewAPIKey,
		KeyID:  resp.NewKeyID,
	}
	if resp.NewKeyExpiresAt != "" {
		cred.ExpiresAt, _ = parseTime(resp.NewKeyExpiresAt)
	}

	return cred, nil
}

// RotateKeyAuto 自动轮换 API Key（使用当前 Key ID，从上下文中推断）。
//
// 先尝试从当前配置中获取 keyId，若不可用则返回错误。
// 调用方应在外层维护 keyId 并在 401 时触发此方法。
func (c *Client) RotateKeyAuto(ctx context.Context, keyID int64) (*DeviceCredential, error) {
	return c.RotateKey(ctx, keyID, 1)
}

// RevokeKey 紧急撤销 API Key。
//
// POST /internal/api/v1/apikeys/{keyId}/revoke
// 鉴权: X-Internal-Token
//
// 撤销后 Key 立即失效，无宽限期。用于安全事件响应。
func (c *Client) RevokeKey(ctx context.Context, keyID int64, reason string) (*RevokeKeyResponse, error) {
	req := RevokeKeyRequest{
		Reason: reason,
	}

	path := fmt.Sprintf("/internal/api/v1/apikeys/%d/revoke", keyID)
	var resp RevokeKeyResponse
	_, _, err := c.doRequest(ctx, "POST", path, req, "internal", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: revoke key %d: %w", keyID, err)
	}

	return &resp, nil
}

// parseTime 解析 ISO 8601 时间字符串。
func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999Z",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("aisaas: cannot parse time %q", s)
}
