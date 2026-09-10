// Package device 实现设备级 API Key 管理与 AES-256-GCM 加密本地存储。
//
// 核心职责：
//   - Device 结构体：设备硬件身份标识
//   - Credentials：设备 API Key 凭证（内存中明文，不落盘）
//   - KeyStore：AES-256-GCM 加密本地存储，密钥从 efuse MAC 派生
//   - Manager：设备 API Key 生命周期管理（申请 / 验证 / 轮换）
//
// 安全模型（Q6 决策）：
//   - AES-256 密钥从 efuse MAC 派生（SHA-256 + 固定 salt）
//   - 加密文件原子写入（write .tmp → fsync → rename）
//   - 内存中仅存明文 API Key，不落盘明文
//   - 文件权限 0600（仅 owner 可读写）
package device

import (
	"errors"
	"time"
)

// Device 表示一个物理设备，包含其硬件身份标识。
//
// 字段说明：
//   - ID：设备唯一标识（由 aisaas 分配）
//   - MACAddress：设备 MAC 地址（来自 Hello 握手）
//   - ChipType：芯片型号（如 ESP32-S3）
//   - Firmware：固件版本
type Device struct {
	ID         string `json:"id"`
	MACAddress string `json:"macAddress"`
	ChipType   string `json:"chipType"`
	Firmware   string `json:"firmware"`
}

// Credentials 设备 API Key 凭证（内存中明文，不落盘）。
//
// 安全约束：
//   - APIKey 仅存内存，不落盘明文
//   - 落盘时整体经 AES-256-GCM 加密
//   - ExpiresAt 用于轮换判断
//   - KeyID 用于 RotateKey 调用
type Credentials struct {
	DeviceID  string    `json:"deviceId"`
	APIKey    string    `json:"apiKey"`
	KeyID     int64     `json:"keyId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

// Config 设备管理器配置。
//
// 字段说明：
//   - EncryptedKeyFile：加密文件路径（默认 "configs/device.key.enc"）
//   - EFUSEMAC：efuse MAC 地址（dev 模式从配置读取，生产环境从设备 Hello 握手获取）
//   - DeviceID：设备唯一标识
//   - ChipType：芯片型号
//   - Firmware：固件版本
type Config struct {
	EncryptedKeyFile string // 加密文件路径，如 "configs/device.key.enc"
	EFUSEMAC         string // efuse MAC 地址（用于 AES-256 密钥派生）
	DeviceID         string // 设备 ID
	ChipType         string // 芯片型号
	Firmware         string // 固件版本
}

// 哨兵错误。
var (
	// ErrKeyFileNotFound 加密文件不存在（首次启动）。
	ErrKeyFileNotFound = errors.New("device: key file not found")
	// ErrKeyFileCorrupted 加密文件损坏（解密失败或 JSON 解析失败）。
	ErrKeyFileCorrupted = errors.New("device: key file corrupted")
	// ErrNotRegistered 设备尚未注册。
	ErrNotRegistered = errors.New("device: not registered")
)