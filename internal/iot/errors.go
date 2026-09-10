package iot

import "errors"

// 哨兵错误。
var (
	// ErrIoTDeviceNotFound 表示 IoT 设备未找到。
	ErrIoTDeviceNotFound = errors.New("iot: device not found")

	// ErrIoTDeviceExists 表示 IoT 设备已注册。
	ErrIoTDeviceExists = errors.New("iot: device already registered")

	// ErrCommandTimeout 表示指令执行超时。
	ErrCommandTimeout = errors.New("iot: command timeout")

	// ErrActionNotSupported 表示设备不支持该动作。
	ErrActionNotSupported = errors.New("iot: action not supported by device")
)