package ota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DeviceRegistry 管理设备激活流程的 Redis 存储。
//
// 存储布局：
//   ota:code:<6位码>        -> JSON{DeviceID, ChipModel, Version, DeviceType, IPAddress, WiFiSSID, IssuedAt}   TTL 5 min
//   ota:device:<deviceId>   -> JSON{Activated, Code, ActivatedAt, ChipModel, Version, DeviceType, IP, WiFiSSID}  无 TTL
//   ota:devices:activated   -> SET 已激活 deviceId（用于 admin 列表）
//   ota:devices:pending     -> SET 待激活 deviceId（code 仍在 TTL 内）
//
// 设计要点：
//   - 码 → 设备 5 分钟过期，过期则需要重新生成码
//   - 设备一旦激活永久保留（除非人工删除）
//   - 两个 SET 用于 admin UI 列表查询，避免 SCAN 全键
type DeviceRegistry struct {
	rdb *redis.Client
}

// DeviceCodeRecord 6 位激活码 → 设备信息（短期，过期后失效）。
type DeviceCodeRecord struct {
	DeviceID   string `json:"deviceId"`
	ChipModel  string `json:"chipModel"`
	Version    string `json:"version"`
	DeviceType string `json:"deviceType"`
	IPAddress  string `json:"ipAddress"`
	WiFiSSID   string `json:"wifiSsid"`
	IssuedAt   int64  `json:"issuedAt"`
}

// DeviceActivationRecord 设备激活状态（持久）。
type DeviceActivationRecord struct {
	Activated   bool   `json:"activated"`
	Code        string `json:"code"`
	ActivatedAt int64  `json:"activatedAt"`
	ChipModel   string `json:"chipModel"`
	Version     string `json:"version"`
	DeviceType  string `json:"deviceType"`
	IPAddress   string `json:"ipAddress"`
	WiFiSSID    string `json:"wifiSsid"`
	DeviceID    string `json:"deviceId"`
}

// ErrCodeNotFound 激活码不存在或已过期。
var ErrCodeNotFound = errors.New("activation code not found or expired")

// NewDeviceRegistry 创建 DeviceRegistry。
func NewDeviceRegistry(rdb *redis.Client) *DeviceRegistry {
	return &DeviceRegistry{rdb: rdb}
}

// SaveCode 保存 6 位激活码 → 设备信息，TTL 5 分钟。同时把 deviceId 加入 pending SET。
func (r *DeviceRegistry) SaveCode(ctx context.Context, code string, rec DeviceCodeRecord) error {
	rec.IssuedAt = time.Now().Unix()
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	key := "ota:code:" + code
	deviceKey := "ota:device:" + rec.DeviceID

	// pipeline: set code (TTL 5min) + add deviceId to pending SET + 暂存 pre-activation record
	pipe := r.rdb.TxPipeline()
	pipe.Set(ctx, key, body, 5*time.Minute)
	pipe.SAdd(ctx, "ota:devices:pending", rec.DeviceID)
	// 暂存设备 pre-activation 信息（让 admin 列表能显示详情，即使码过期）
	preRec := DeviceActivationRecord{
		Activated:   false,
		Code:        code,
		ChipModel:   rec.ChipModel,
		Version:     rec.Version,
		DeviceType:  rec.DeviceType,
		IPAddress:   rec.IPAddress,
		WiFiSSID:    rec.WiFiSSID,
		DeviceID:    rec.DeviceID,
		ActivatedAt: 0,
	}
	preBody, _ := json.Marshal(preRec)
	pipe.Set(ctx, deviceKey, preBody, 24*time.Hour)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline: %w", err)
	}
	return nil
}

// LookupCode 用 6 位激活码查设备信息。
func (r *DeviceRegistry) LookupCode(ctx context.Context, code string) (*DeviceCodeRecord, error) {
	key := "ota:code:" + code
	body, err := r.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, ErrCodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}
	var rec DeviceCodeRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &rec, nil
}

// ActivateByCode 用 6 位码激活设备。返回激活的 deviceId。
func (r *DeviceRegistry) ActivateByCode(ctx context.Context, code string) (string, error) {
	rec, err := r.LookupCode(ctx, code)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	deviceKey := "ota:device:" + rec.DeviceID
	activated := DeviceActivationRecord{
		Activated:   true,
		Code:        code,
		ActivatedAt: now,
		ChipModel:   rec.ChipModel,
		Version:     rec.Version,
		DeviceType:  rec.DeviceType,
		IPAddress:   rec.IPAddress,
		WiFiSSID:    rec.WiFiSSID,
		DeviceID:    rec.DeviceID,
	}
	body, _ := json.Marshal(activated)
	pipe := r.rdb.TxPipeline()
	pipe.Set(ctx, deviceKey, body, 0) // 永久
	pipe.SRem(ctx, "ota:devices:pending", rec.DeviceID)
	pipe.SAdd(ctx, "ota:devices:activated", rec.DeviceID)
	pipe.Del(ctx, "ota:code:"+code) // 一次性
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("activate pipeline: %w", err)
	}
	return rec.DeviceID, nil
}

// IsActivated 检查设备是否已激活。
func (r *DeviceRegistry) IsActivated(ctx context.Context, deviceID string) (bool, *DeviceActivationRecord, error) {
	deviceKey := "ota:device:" + deviceID
	body, err := r.rdb.Get(ctx, deviceKey).Bytes()
	if err == redis.Nil {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("redis get: %w", err)
	}
	var rec DeviceActivationRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return false, nil, fmt.Errorf("unmarshal: %w", err)
	}
	return rec.Activated, &rec, nil
}

// ListPending 列出所有待激活设备（含详情）。
func (r *DeviceRegistry) ListPending(ctx context.Context) ([]DeviceActivationRecord, error) {
	ids, err := r.rdb.SMembers(ctx, "ota:devices:pending").Result()
	if err != nil {
		return nil, err
	}
	return r.batchGet(ctx, ids)
}

// ListActivated 列出所有已激活设备。
func (r *DeviceRegistry) ListActivated(ctx context.Context) ([]DeviceActivationRecord, error) {
	ids, err := r.rdb.SMembers(ctx, "ota:devices:activated").Result()
	if err != nil {
		return nil, err
	}
	return r.batchGet(ctx, ids)
}

func (r *DeviceRegistry) batchGet(ctx context.Context, ids []string) ([]DeviceActivationRecord, error) {
	if len(ids) == 0 {
		return []DeviceActivationRecord{}, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = "ota:device:" + id
	}
	bodies, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]DeviceActivationRecord, 0, len(ids))
	for _, raw := range bodies {
		s, ok := raw.(string)
		if !ok || s == "" {
			continue
		}
		var rec DeviceActivationRecord
		if err := json.Unmarshal([]byte(s), &rec); err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}