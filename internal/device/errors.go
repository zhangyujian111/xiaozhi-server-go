package device

import "errors"

// DeviceManager 哨兵错误。
// 注意：这些错误与 device.go 中现有 Manager（API Key 管理）的哨兵错误属于同一包，职责分离。
var (
	// ErrDeviceNotFound 设备元数据不存在。
	ErrDeviceNotFound = errors.New("device: not found")

	// ErrDeviceExists 设备已注册（MAC 地址重复）。
	ErrDeviceExists = errors.New("device: already exists")

	// ErrDeviceOffline 设备离线。
	ErrDeviceOffline = errors.New("device: device is offline")
)