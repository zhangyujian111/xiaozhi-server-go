// Package iot 提供 IoT 设备控制接口与实现。
//
// 职责：
//   - IoT 设备指令下发（turn_on / turn_off / set_volume 等）
//   - IoT 设备注册与发现
//   - 指令执行结果追踪
//
// 传输层决策：
//   - P2 阶段：mock 50ms 延迟（不连接 MQTT broker）
//   - P3 阶段：升级到 MQTT transport（broker URL 通过 config 配置）
package iot

import (
	"context"
	"time"
)

// Manager IoT 设备管理接口。
//
// 职责：
//   - IoT 设备指令下发（turn_on / turn_off / set_volume 等）
//   - IoT 设备注册与发现
//   - 指令执行结果追踪
//
// 传输层决策：
//   - 默认使用 MQTT broker（broker URL 通过配置指定）
//   - 支持 QoS 1（至少一次送达）
//   - 指令超时后自动标记失败
type Manager interface {
	// Execute 向 IoT 设备下发控制指令。
	//
	// 参数：
	//   - deviceID：目标 IoT 设备 ID
	//   - cmd：控制指令（Action + Parameters）
	//
	// 返回：执行结果（含延迟和输出）。
	// 超时时间内未收到设备响应返回 ErrCommandTimeout。
	Execute(ctx context.Context, deviceID string, cmd Command) (*Result, error)

	// RegisterDevice 注册 IoT 设备到 MQTT broker。
	//
	// 注册后可通过 Execute 下发指令。
	// 同 deviceID 重复注册返回 ErrIoTDeviceExists。
	RegisterDevice(ctx context.Context, device IoTDevice) error

	// GetDevice 查询 IoT 设备信息。
	GetDevice(ctx context.Context, deviceID string) (*IoTDevice, error)

	// ListDevices 查询 IoT 设备列表。
	ListDevices(ctx context.Context, filter DeviceFilter) ([]*IoTDevice, error)

	// UnregisterDevice 注销 IoT 设备。
	UnregisterDevice(ctx context.Context, deviceID string) error

	// GetStatus 获取 IoT 设备当前状态。
	//
	// 返回设备上次上报的属性快照。
	GetStatus(ctx context.Context, deviceID string) (*DeviceStatus, error)
}

// Command IoT 控制指令。
type Command struct {
	Action     string         `json:"action"`     // 动作名：turn_on / turn_off / set_volume / set_brightness / set_temperature
	Parameters map[string]any `json:"parameters"` // 指令参数（如 {"volume": 80}）
	Timeout    time.Duration  `json:"timeout"`    // 超时时间（默认 5s）
	QoS        byte           `json:"qos"`        // MQTT QoS（0/1/2，默认 1）
}

// Result 指令执行结果。
type Result struct {
	Success   bool           `json:"success"`   // 是否成功
	Output    map[string]any `json:"output"`    // 设备返回的输出数据
	Error     string         `json:"error"`     // 错误信息（成功时为空）
	LatencyMs int64          `json:"latencyMs"` // 执行延迟（毫秒）
}

// IoTDevice IoT 设备信息。
type IoTDevice struct {
	ID           string         `json:"id"`           // 设备唯一标识
	Name         string         `json:"name"`         // 设备名称
	Type         string         `json:"type"`         // 设备类型：light / switch / curtain / thermostat / sensor
	Manufacturer string         `json:"manufacturer"` // 制造商
	Model        string         `json:"model"`        // 设备型号
	MQTTTopic    string         `json:"mqttTopic"`    // MQTT 订阅主题
	Actions      []string       `json:"actions"`      // 支持的动作列表
	Properties   map[string]any `json:"properties"`   // 设备属性（亮度范围、温度范围等）
	RegisteredAt time.Time      `json:"registeredAt"` // 注册时间
	LastSeenAt   time.Time      `json:"lastSeenAt"`   // 最后在线时间
}

// DeviceStatus IoT 设备当前状态。
type DeviceStatus struct {
	DeviceID  string         `json:"deviceId"`  // 设备 ID
	Online    bool           `json:"online"`    // 是否在线
	State     map[string]any `json:"state"`     // 当前状态快照（如 {"power": "on", "volume": 50}）
	UpdatedAt time.Time      `json:"updatedAt"` // 状态更新时间
}

// DeviceFilter IoT 设备过滤条件。
type DeviceFilter struct {
	Type     string `json:"type"`     // 设备类型过滤
	Online   *bool  `json:"online"`   // 在线状态过滤（nil 表示全部）
	Keyword  string `json:"keyword"`  // 搜索关键词
	Page     int    `json:"page"`     // 页码
	PageSize int    `json:"pageSize"` // 每页条数
}