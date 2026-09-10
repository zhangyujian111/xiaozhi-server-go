// Package device 设备元数据管理扩展。
//
// 本文件实现 P2 接口设计中的 DeviceManager 接口，
// 负责设备元数据 CRUD、人设绑定、在线状态追踪。
//
// 与现有 Manager（API Key 管理）职责分离：
//   - Manager：设备 API Key 生命周期（注册/验证/轮换）
//   - DeviceManager：设备元数据管理（CRUD/人设/状态/固件）
//
// 线程安全：所有公共方法受 sync.RWMutex 保护。
package device

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ============================================================
// DeviceManager — 设备元数据管理接口
// ============================================================

// DeviceManager 设备元数据管理接口。
//
// 职责：
//   - 设备 CRUD
//   - 人设（Persona）绑定 / 解绑
//   - 设备在线状态追踪
//   - 设备固件信息同步
//
// 注意：此接口与现有 device.Manager（API Key 管理）职责分离。
// 现有 Manager 专注于 API Key 生命周期，DeviceManager 专注于设备元数据。
type DeviceManager interface {
	// Register 注册新设备。
	//
	// 设备通过 MAC 地址全局唯一标识。
	// 重复注册返回 ErrDeviceExists。
	Register(ctx context.Context, req RegisterDeviceReq) (*DeviceInfo, error)

	// Get 根据设备 ID 查询设备元数据。
	//
	// 返回：设备信息，不存在时返回 ErrDeviceNotFound。
	Get(ctx context.Context, deviceID string) (*DeviceInfo, error)

	// GetByMAC 根据 MAC 地址查询设备。
	GetByMAC(ctx context.Context, macAddress string) (*DeviceInfo, error)

	// Update 更新设备元数据。
	//
	// 仅更新非零值字段。
	Update(ctx context.Context, deviceID string, req UpdateDeviceReq) (*DeviceInfo, error)

	// Delete 删除设备（软删除）。
	//
	// 删除前需解绑所有人设和 MQTT 订阅。
	Delete(ctx context.Context, deviceID string) error

	// List 分页查询设备列表。
	List(ctx context.Context, filter DeviceFilter) (*DevicePage, error)

	// BindPersona 为设备绑定人设。
	//
	// 同一设备可绑定多个人设，其中 isDefault=true 的为默认人设。
	BindPersona(ctx context.Context, deviceID, personaID string, isDefault bool) error

	// UnbindPersona 解绑设备的人设。
	UnbindPersona(ctx context.Context, deviceID, personaID string) error

	// GetPersonas 获取设备绑定的所有人设。
	GetPersonas(ctx context.Context, deviceID string) ([]*PersonaBinding, error)

	// UpdateStatus 更新设备在线状态。
	//
	// 设备连接/断开时调用，自动更新 lastSeenAt。
	UpdateStatus(ctx context.Context, deviceID string, status DeviceStatus) error

	// SyncFirmware 同步设备固件信息。
	//
	// 设备 Hello 握手后调用，更新固件版本、芯片型号等。
	SyncFirmware(ctx context.Context, deviceID string, req SyncFirmwareReq) error
}

// ============================================================
// 数据结构
// ============================================================

// DeviceInfo 设备元数据。
type DeviceInfo struct {
	ID            string       `json:"id"`            // 设备唯一标识
	MACAddress    string       `json:"macAddress"`    // MAC 地址
	DeviceName    string       `json:"deviceName"`    // 设备名称
	ChipType      string       `json:"chipType"`      // 芯片型号（如 ESP32-S3）
	ChipModelName string       `json:"chipModelName"` // 芯片完整型号名
	Firmware      string       `json:"firmware"`      // 固件版本
	IPAddress     string       `json:"ipAddress"`     // 最近 IP 地址
	WiFiSSID      string       `json:"wifiSsid"`      // WiFi SSID
	Location      string       `json:"location"`      // 地理位置（IP 解析）
	DeviceType    string       `json:"deviceType"`    // 设备类型（esp32/linux/web）
	TenantID      string       `json:"tenantId"`      // 所属租户 ID
	OwnerUserID   string       `json:"ownerUserId"`   // 所属用户 ID
	Status        DeviceStatus `json:"status"`        // 在线状态
	LastSeenAt    *time.Time   `json:"lastSeenAt"`    // 最后在线时间
	RegisteredAt  time.Time    `json:"registeredAt"`  // 注册时间
	UpdatedAt     time.Time    `json:"updatedAt"`     // 更新时间
}

// DeviceStatus 设备在线状态。
type DeviceStatus string

const (
	DeviceStatusOnline  DeviceStatus = "online"  // 在线
	DeviceStatusOffline DeviceStatus = "offline" // 离线
	DeviceStatusError   DeviceStatus = "error"   // 异常
)

// PersonaBinding 设备人设绑定关系。
type PersonaBinding struct {
	DeviceID  string    `json:"deviceId"`  // 设备 ID
	PersonaID string    `json:"personaId"` // 人设 ID
	IsDefault bool      `json:"isDefault"` // 是否为默认人设
	BoundAt   time.Time `json:"boundAt"`   // 绑定时间
}

// RegisterDeviceReq 设备注册请求。
type RegisterDeviceReq struct {
	MACAddress    string `json:"macAddress"`    // MAC 地址（必填）
	DeviceName    string `json:"deviceName"`    // 设备名称
	ChipType      string `json:"chipType"`      // 芯片型号
	ChipModelName string `json:"chipModelName"` // 芯片完整型号名
	Firmware      string `json:"firmware"`      // 固件版本
	DeviceType    string `json:"deviceType"`    // 设备类型
	TenantID      string `json:"tenantId"`      // 租户 ID
	OwnerUserID   string `json:"ownerUserId"`   // 所属用户 ID
}

// UpdateDeviceReq 更新设备请求。
type UpdateDeviceReq struct {
	DeviceName  *string `json:"deviceName"`  // 设备名称
	Location    *string `json:"location"`    // 地理位置
	OwnerUserID *string `json:"ownerUserId"` // 转移所属用户
}

// SyncFirmwareReq 固件同步请求。
type SyncFirmwareReq struct {
	Firmware      string `json:"firmware"`      // 固件版本
	ChipType      string `json:"chipType"`      // 芯片型号
	ChipModelName string `json:"chipModelName"` // 芯片完整型号名
	DeviceType    string `json:"deviceType"`    // 设备类型
	WiFiSSID      string `json:"wifiSsid"`      // WiFi SSID
	IPAddress     string `json:"ipAddress"`     // IP 地址
	Location      string `json:"location"`      // 地理位置
}

// DeviceFilter 设备查询过滤条件。
type DeviceFilter struct {
	Keyword     string        `json:"keyword"`     // 搜索关键词（设备名/ID/MAC）
	Status      *DeviceStatus `json:"status"`      // 状态过滤
	TenantID    string        `json:"tenantId"`    // 租户 ID 过滤
	OwnerUserID string        `json:"ownerUserId"` // 所属用户过滤
	DeviceType  string        `json:"deviceType"`  // 设备类型过滤
	PersonaID   string        `json:"personaId"`   // 人设 ID 过滤（查询绑定该人设的设备）
	Page        int           `json:"page"`        // 页码
	PageSize    int           `json:"pageSize"`    // 每页条数
}

// DevicePage 设备分页结果。
type DevicePage struct {
	Items    []*DeviceInfo `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
	HasMore  bool          `json:"hasMore"`
}

// ============================================================
// deviceManagerImpl — 内存实现
// ============================================================

// deviceManagerImpl 内存设备元数据管理器。
//
// 线程安全：所有公共方法受 mu 保护。
type deviceManagerImpl struct {
	mu              sync.RWMutex
	devices         map[string]*DeviceInfo   // deviceID → DeviceInfo
	macIndex        map[string]string        // MAC → deviceID
	personaBindings map[string]map[string]*PersonaBinding // deviceID → personaID → binding
	logger          *slog.Logger
}

// NewDeviceManager 创建内存设备元数据管理器。
//
// 参数：
//   - logger：slog 日志器（nil 则使用默认）
//
// 返回：DeviceManager 接口实例。
func NewDeviceManager(logger *slog.Logger) DeviceManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &deviceManagerImpl{
		devices:         make(map[string]*DeviceInfo),
		macIndex:        make(map[string]string),
		personaBindings: make(map[string]map[string]*PersonaBinding),
		logger:          logger.With("component", "device.devicemanager"),
	}
}

// Register 注册新设备。
func (m *deviceManagerImpl) Register(ctx context.Context, req RegisterDeviceReq) (*DeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查 MAC 地址唯一性
	if req.MACAddress != "" {
		if _, exists := m.macIndex[req.MACAddress]; exists {
			return nil, ErrDeviceExists
		}
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("device: generate uuid: %w", err)
	}

	now := time.Now()
	dev := &DeviceInfo{
		ID:            id.String(),
		MACAddress:    req.MACAddress,
		DeviceName:    req.DeviceName,
		ChipType:      req.ChipType,
		ChipModelName: req.ChipModelName,
		Firmware:      req.Firmware,
		IPAddress:     "",
		WiFiSSID:      "",
		Location:      "",
		DeviceType:    req.DeviceType,
		TenantID:      req.TenantID,
		OwnerUserID:   req.OwnerUserID,
		Status:        DeviceStatusOffline,
		LastSeenAt:    nil,
		RegisteredAt:  now,
		UpdatedAt:     now,
	}

	m.devices[dev.ID] = dev
	if dev.MACAddress != "" {
		m.macIndex[dev.MACAddress] = dev.ID
	}
	m.personaBindings[dev.ID] = make(map[string]*PersonaBinding)

	m.logger.InfoContext(ctx, "device registered",
		"deviceId", dev.ID,
		"mac", dev.MACAddress,
		"deviceType", dev.DeviceType,
	)

	return copyDeviceInfo(dev), nil
}

// Get 根据设备 ID 查询设备元数据。
func (m *deviceManagerImpl) Get(ctx context.Context, deviceID string) (*DeviceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return nil, ErrDeviceNotFound
	}

	return copyDeviceInfo(dev), nil
}

// GetByMAC 根据 MAC 地址查询设备。
func (m *deviceManagerImpl) GetByMAC(ctx context.Context, macAddress string) (*DeviceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	deviceID, ok := m.macIndex[macAddress]
	if !ok {
		return nil, ErrDeviceNotFound
	}

	dev, ok := m.devices[deviceID]
	if !ok {
		return nil, ErrDeviceNotFound
	}

	return copyDeviceInfo(dev), nil
}

// Update 更新设备元数据（仅更新非零值字段）。
func (m *deviceManagerImpl) Update(ctx context.Context, deviceID string, req UpdateDeviceReq) (*DeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return nil, ErrDeviceNotFound
	}

	if req.DeviceName != nil {
		dev.DeviceName = *req.DeviceName
	}
	if req.Location != nil {
		dev.Location = *req.Location
	}
	if req.OwnerUserID != nil {
		dev.OwnerUserID = *req.OwnerUserID
	}

	dev.UpdatedAt = time.Now()

	m.logger.InfoContext(ctx, "device updated",
		"deviceId", dev.ID,
	)

	return copyDeviceInfo(dev), nil
}

// Delete 删除设备（软删除，解绑所有人设）。
func (m *deviceManagerImpl) Delete(ctx context.Context, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return ErrDeviceNotFound
	}

	// 清理 MAC 索引
	if dev.MACAddress != "" {
		delete(m.macIndex, dev.MACAddress)
	}

	// 清理人设绑定
	delete(m.personaBindings, deviceID)

	// 删除设备记录
	delete(m.devices, deviceID)

	m.logger.InfoContext(ctx, "device deleted",
		"deviceId", deviceID,
		"mac", dev.MACAddress,
	)

	return nil
}

// List 分页查询设备列表。
func (m *deviceManagerImpl) List(ctx context.Context, filter DeviceFilter) (*DevicePage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 设置默认分页参数
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}

	// 如果按人设过滤，先收集绑定了该人设的设备 ID
	var personaDeviceIDs map[string]bool
	if filter.PersonaID != "" {
		personaDeviceIDs = make(map[string]bool)
		for devID, bindings := range m.personaBindings {
			if _, ok := bindings[filter.PersonaID]; ok {
				personaDeviceIDs[devID] = true
			}
		}
	}

	// 收集匹配的设备
	var matched []*DeviceInfo
	for _, dev := range m.devices {
		// 人设过滤
		if filter.PersonaID != "" && !personaDeviceIDs[dev.ID] {
			continue
		}

		// 关键词过滤
		if filter.Keyword != "" {
			kw := strings.ToLower(filter.Keyword)
			if !strings.Contains(strings.ToLower(dev.DeviceName), kw) &&
				!strings.Contains(strings.ToLower(dev.ID), kw) &&
				!strings.Contains(strings.ToLower(dev.MACAddress), kw) {
				continue
			}
		}

		// 状态过滤
		if filter.Status != nil && dev.Status != *filter.Status {
			continue
		}

		// 租户过滤
		if filter.TenantID != "" && dev.TenantID != filter.TenantID {
			continue
		}

		// 所属用户过滤
		if filter.OwnerUserID != "" && dev.OwnerUserID != filter.OwnerUserID {
			continue
		}

		// 设备类型过滤
		if filter.DeviceType != "" && dev.DeviceType != filter.DeviceType {
			continue
		}

		matched = append(matched, copyDeviceInfo(dev))
	}

	total := int64(len(matched))

	// 分页
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(matched) {
		return &DevicePage{
			Items:    []*DeviceInfo{},
			Total:    total,
			Page:     filter.Page,
			PageSize: filter.PageSize,
			HasMore:  false,
		}, nil
	}

	end := start + filter.PageSize
	if end > len(matched) {
		end = len(matched)
	}

	return &DevicePage{
		Items:    matched[start:end],
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		HasMore:  end < len(matched),
	}, nil
}

// BindPersona 为设备绑定人设。
func (m *deviceManagerImpl) BindPersona(ctx context.Context, deviceID, personaID string, isDefault bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return ErrDeviceNotFound
	}

	// 如果设置为默认人设，先取消其他默认
	if isDefault {
		for pid, binding := range m.personaBindings[deviceID] {
			if binding.IsDefault {
				binding.IsDefault = false
				_ = pid
			}
		}
	}

	m.personaBindings[deviceID][personaID] = &PersonaBinding{
		DeviceID:  deviceID,
		PersonaID: personaID,
		IsDefault: isDefault,
		BoundAt:   time.Now(),
	}

	dev.UpdatedAt = time.Now()

	m.logger.InfoContext(ctx, "persona bound to device",
		"deviceId", deviceID,
		"personaId", personaID,
		"isDefault", isDefault,
	)

	return nil
}

// UnbindPersona 解绑设备的人设。
func (m *deviceManagerImpl) UnbindPersona(ctx context.Context, deviceID, personaID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.devices[deviceID]; !ok {
		return ErrDeviceNotFound
	}

	bindings, ok := m.personaBindings[deviceID]
	if !ok {
		return nil // 人设未绑定，视为成功
	}

	delete(bindings, personaID)

	m.logger.InfoContext(ctx, "persona unbound from device",
		"deviceId", deviceID,
		"personaId", personaID,
	)

	return nil
}

// GetPersonas 获取设备绑定的所有人设。
func (m *deviceManagerImpl) GetPersonas(ctx context.Context, deviceID string) ([]*PersonaBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.devices[deviceID]; !ok {
		return nil, ErrDeviceNotFound
	}

	bindings, ok := m.personaBindings[deviceID]
	if !ok {
		return []*PersonaBinding{}, nil
	}

	result := make([]*PersonaBinding, 0, len(bindings))
	for _, b := range bindings {
		cp := *b
		result = append(result, &cp)
	}

	return result, nil
}

// UpdateStatus 更新设备在线状态。
func (m *deviceManagerImpl) UpdateStatus(ctx context.Context, deviceID string, status DeviceStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return ErrDeviceNotFound
	}

	now := time.Now()
	dev.Status = status
	dev.LastSeenAt = &now
	dev.UpdatedAt = now

	m.logger.InfoContext(ctx, "device status updated",
		"deviceId", deviceID,
		"status", status,
	)

	return nil
}

// SyncFirmware 同步设备固件信息。
func (m *deviceManagerImpl) SyncFirmware(ctx context.Context, deviceID string, req SyncFirmwareReq) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return ErrDeviceNotFound
	}

	if req.Firmware != "" {
		dev.Firmware = req.Firmware
	}
	if req.ChipType != "" {
		dev.ChipType = req.ChipType
	}
	if req.ChipModelName != "" {
		dev.ChipModelName = req.ChipModelName
	}
	if req.DeviceType != "" {
		dev.DeviceType = req.DeviceType
	}
	if req.WiFiSSID != "" {
		dev.WiFiSSID = req.WiFiSSID
	}
	if req.IPAddress != "" {
		dev.IPAddress = req.IPAddress
	}
	if req.Location != "" {
		dev.Location = req.Location
	}

	dev.UpdatedAt = time.Now()

	m.logger.InfoContext(ctx, "device firmware synced",
		"deviceId", deviceID,
		"firmware", dev.Firmware,
		"chipType", dev.ChipType,
	)

	return nil
}

// copyDeviceInfo 深拷贝设备信息。
func copyDeviceInfo(d *DeviceInfo) *DeviceInfo {
	cp := *d
	if d.LastSeenAt != nil {
		t := *d.LastSeenAt
		cp.LastSeenAt = &t
	}
	return &cp
}