package iot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// manager IoT 设备管理器（内存实现）。
//
// P2 阶段：设备注册到内存 map，Execute 模拟 50ms 延迟后返回成功。
// P3 阶段：升级到 MQTT transport（通过 config.MQTT.Broker 连接）。
type manager struct {
	mu      sync.RWMutex
	devices map[string]*IoTDevice  // deviceID → IoTDevice
	status  map[string]*DeviceStatus // deviceID → DeviceStatus
	logger  *slog.Logger
}

// NewManager 创建 IoT 设备管理器。
func NewManager(logger *slog.Logger) Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &manager{
		devices: make(map[string]*IoTDevice),
		status:  make(map[string]*DeviceStatus),
		logger:  logger,
	}
}

// Execute 向 IoT 设备下发控制指令（P2 mock：50ms 延迟后返回成功）。
func (m *manager) Execute(ctx context.Context, deviceID string, cmd Command) (*Result, error) {
	m.mu.RLock()
	dev, ok := m.devices[deviceID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrIoTDeviceNotFound
	}

	// 检查设备是否支持该动作
	actionSupported := false
	for _, action := range dev.Actions {
		if action == cmd.Action {
			actionSupported = true
			break
		}
	}
	if !actionSupported {
		return nil, ErrActionNotSupported
	}

	// 模拟 50ms 执行延迟
	start := time.Now()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}

	latency := time.Since(start).Milliseconds()

	m.logger.InfoContext(ctx, "iot command executed",
		"device_id", deviceID,
		"action", cmd.Action,
		"params", cmd.Parameters,
		"latency_ms", latency,
	)

	// 更新设备状态
	m.mu.Lock()
	if ds, exists := m.status[deviceID]; exists {
		if ds.State == nil {
			ds.State = make(map[string]any)
		}
		// 根据 action 更新状态快照
		switch cmd.Action {
		case "turn_on":
			ds.State["power"] = "on"
		case "turn_off":
			ds.State["power"] = "off"
		case "set_volume":
			if v, ok := cmd.Parameters["volume"]; ok {
				ds.State["volume"] = v
			}
		case "set_brightness":
			if v, ok := cmd.Parameters["brightness"]; ok {
				ds.State["brightness"] = v
			}
		case "set_temperature":
			if v, ok := cmd.Parameters["temperature"]; ok {
				ds.State["temperature"] = v
			}
		}
		ds.UpdatedAt = time.Now()
	}
	m.mu.Unlock()

	return &Result{
		Success:   true,
		Output:    map[string]any{"status": "ok"},
		LatencyMs: latency,
	}, nil
}

// RegisterDevice 注册 IoT 设备到内存。
func (m *manager) RegisterDevice(ctx context.Context, device IoTDevice) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.devices[device.ID]; exists {
		return ErrIoTDeviceExists
	}

	if device.ID == "" {
		device.ID = uuid.New().String()
	}
	device.RegisteredAt = time.Now()
	device.LastSeenAt = time.Now()

	m.devices[device.ID] = &device
	m.status[device.ID] = &DeviceStatus{
		DeviceID:  device.ID,
		Online:    true,
		State:     make(map[string]any),
		UpdatedAt: time.Now(),
	}

	m.logger.InfoContext(ctx, "iot device registered",
		"device_id", device.ID,
		"name", device.Name,
		"type", device.Type,
	)

	return nil
}

// GetDevice 查询 IoT 设备信息。
func (m *manager) GetDevice(ctx context.Context, deviceID string) (*IoTDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return nil, ErrIoTDeviceNotFound
	}

	// 返回副本防止外部修改
	copy := *dev
	return &copy, nil
}

// ListDevices 查询 IoT 设备列表。
func (m *manager) ListDevices(ctx context.Context, filter DeviceFilter) ([]*IoTDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*IoTDevice, 0)

	for _, dev := range m.devices {
		// 类型过滤
		if filter.Type != "" && dev.Type != filter.Type {
			continue
		}
		// 在线状态过滤
		if filter.Online != nil {
			ds, hasStatus := m.status[dev.ID]
			if hasStatus && ds.Online != *filter.Online {
				continue
			}
		}
		// 关键词过滤
		if filter.Keyword != "" {
			matched := false
			k := filter.Keyword
			if contains(dev.ID, k) || contains(dev.Name, k) || contains(dev.Manufacturer, k) || contains(dev.Model, k) {
				matched = true
			}
			if !matched {
				continue
			}
		}

		// 返回副本
		copy := *dev
		result = append(result, &copy)
	}

	// 简单分页
	if filter.Page > 0 && filter.PageSize > 0 {
		start := (filter.Page - 1) * filter.PageSize
		if start >= len(result) {
			return []*IoTDevice{}, nil
		}
		end := start + filter.PageSize
		if end > len(result) {
			end = len(result)
		}
		result = result[start:end]
	}

	return result, nil
}

// UnregisterDevice 注销 IoT 设备。
func (m *manager) UnregisterDevice(ctx context.Context, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.devices[deviceID]; !ok {
		return ErrIoTDeviceNotFound
	}

	delete(m.devices, deviceID)
	delete(m.status, deviceID)

	m.logger.InfoContext(ctx, "iot device unregistered", "device_id", deviceID)

	return nil
}

// GetStatus 获取 IoT 设备当前状态。
func (m *manager) GetStatus(ctx context.Context, deviceID string) (*DeviceStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ds, ok := m.status[deviceID]
	if !ok {
		return nil, ErrIoTDeviceNotFound
	}

	// 返回副本
	copy := *ds
	if copy.State == nil {
		copy.State = make(map[string]any)
	}
	return &copy, nil
}

// contains 简单子串匹配（不区分大小写）。
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
			// 不区分大小写
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}