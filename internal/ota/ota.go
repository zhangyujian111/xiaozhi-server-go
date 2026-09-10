// Package ota 提供 OTA 固件分发服务接口与实现。
//
// 职责：
//   - 固件版本检查（设备端轮询）
//   - 固件激活状态查询
//   - 固件上传与管理
//   - 设备激活码生成
//
// 对齐 Java DeviceAppService.handleOta / checkOtaActivation 逻辑。
//
// P2 阶段：内存 mock 固件元数据 + HTTP 直接下载。
// P3 阶段：升级到 OSS 签名 URL + aisaas 固件元数据管理。
package ota

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Service OTA 固件分发服务接口。
//
// 职责：
//   - 固件版本检查（设备端轮询）
//   - 固件激活状态查询
//   - 固件上传与管理
//   - 设备激活码生成
//
// 对齐 Java DeviceAppService.handleOta / checkOtaActivation 逻辑。
type Service interface {
	// CheckUpdate 检查固件更新。
	//
	// 对应 GET /api/device/ota?deviceId=xxx&version=yyy
	// 设备启动时调用，比较当前版本与最新固件版本。
	//
	// 返回：
	//   - HasUpdate=true：有新固件可用，返回下载 URL 和变更日志
	//   - HasUpdate=false：已是当前最新版本
	CheckUpdate(ctx context.Context, req CheckUpdateReq) (*CheckUpdateResp, error)

	// Activate 查询 OTA 激活状态。
	//
	// 对应 POST /api/device/ota/activate
	// 设备首次激活后查询是否已绑定。
	//
	// 返回：
	//   - 已激活：返回 websocket 地址和 token
	//   - 未激活：返回激活码（activation code）
	Activate(ctx context.Context, req ActivateReq) (*ActivateResp, error)

	// GenerateActivationCode 为设备生成激活码。
	//
	// 用于未绑定设备的首次激活流程。
	// 激活码有效期 5 分钟。
	GenerateActivationCode(ctx context.Context, deviceID, deviceType string) (*ActivationCode, error)

	// UploadFirmware 上传新固件。
	//
	// 参数：
	//   - req：固件信息 + 二进制数据
	//
	// 返回：固件记录。
	// 同一 chipModel + version 组合不可重复上传。
	UploadFirmware(ctx context.Context, req UploadFirmwareReq) (*Firmware, error)

	// ListFirmwares 查询固件列表。
	ListFirmwares(ctx context.Context, filter FirmwareFilter) ([]*Firmware, error)

	// GetFirmware 获取固件详情。
	GetFirmware(ctx context.Context, firmwareID string) (*Firmware, error)

	// DeleteFirmware 删除固件（软删除）。
	DeleteFirmware(ctx context.Context, firmwareID string) error

	// SetMandatory 设置固件为强制更新。
	//
	// 强制更新意味着设备必须升级到此版本，不可跳过。
	SetMandatory(ctx context.Context, firmwareID string, mandatory bool) error

	// HandleCheckUpdate 处理 GET /api/device/ota?deviceId=xxx&version=yyy
	HandleCheckUpdate(c *gin.Context)

	// HandleActivate 处理 POST /api/device/ota/activate
	HandleActivate(c *gin.Context)

	// HandleFirmwareDownload 处理固件文件下载 GET /firmware/{firmwareId}.bin
	HandleFirmwareDownload(c *gin.Context)
}

// CheckUpdateReq 固件检查请求。
type CheckUpdateReq struct {
	DeviceID   string `json:"deviceId"`   // 设备 ID（必填）
	ChipModel  string `json:"chipModel"`  // 芯片型号（如 ESP32-S3）
	CurrentVer string `json:"currentVer"` // 当前固件版本
	DeviceType string `json:"deviceType"` // 设备类型
}

// CheckUpdateResp 固件检查响应。
type CheckUpdateResp struct {
	HasUpdate   bool   `json:"hasUpdate"`   // 是否有更新
	FirmwareURL string `json:"firmwareUrl"` // 固件下载 URL
	Version     string `json:"version"`     // 最新版本号
	Mandatory   bool   `json:"mandatory"`   // 是否强制更新
	Changelog   string `json:"changelog"`   // 变更日志
	Checksum    string `json:"checksum"`    // SHA-256 校验和
	Size        int64  `json:"size"`        // 固件大小（字节）
}

// ActivateReq 激活请求。
type ActivateReq struct {
	DeviceID   string `json:"deviceId"`   // 设备 ID
	ChipModel  string `json:"chipModel"`  // 芯片型号
	DeviceType string `json:"deviceType"` // 设备类型
	IPAddress  string `json:"ipAddress"`  // 设备 IP
	WiFiSSID   string `json:"wifiSsid"`   // WiFi SSID
	Version    string `json:"version"`    // 固件版本
}

// ActivateResp 激活响应。
type ActivateResp struct {
	Activated  bool            `json:"activated"`  // 是否已激活
	Activation *ActivationCode `json:"activation"` // 激活码（未激活时返回）
	WebSocket  *WebSocketInfo  `json:"websocket"`  // WebSocket 连接信息（已激活时返回）
	ServerTime *ServerTimeInfo `json:"serverTime"` // 服务端时间
	Firmware   *FirmwareBrief  `json:"firmware"`   // 固件信息
}

// ActivationCode 设备激活码。
type ActivationCode struct {
	Code      string    `json:"code"`      // 6 位激活码
	Message   string    `json:"message"`   // 激活提示信息
	Challenge string    `json:"challenge"` // 挑战码（设备 ID）
	ExpiresAt time.Time `json:"expiresAt"` // 过期时间
}

// WebSocketInfo WebSocket 连接信息。
type WebSocketInfo struct {
	URL   string `json:"url"`   // WebSocket 地址
	Token string `json:"token"` // 连接 Token
}

// ServerTimeInfo 服务端时间信息。
type ServerTimeInfo struct {
	Timestamp      int64 `json:"timestamp"`      // Unix 时间戳（毫秒）
	TimezoneOffset int   `json:"timezoneOffset"` // 时区偏移（分钟）
}

// FirmwareBrief 固件简要信息。
type FirmwareBrief struct {
	URL     string `json:"url"`     // 固件下载地址
	Version string `json:"version"` // 固件版本
}

// UploadFirmwareReq 固件上传请求。
type UploadFirmwareReq struct {
	Version    string `json:"version"`    // 固件版本号（必填）
	ChipModel  string `json:"chipModel"`  // 目标芯片型号（必填）
	DeviceType string `json:"deviceType"` // 设备类型
	Changelog  string `json:"changelog"`  // 变更日志
	Mandatory  bool   `json:"mandatory"`  // 是否强制更新
	FileData   []byte `json:"-"`          // 固件二进制数据（不序列化）
	FileName   string `json:"fileName"`   // 固件文件名
	FileSize   int64  `json:"fileSize"`   // 文件大小
	Checksum   string `json:"checksum"`   // SHA-256（服务端校验）
}

// Firmware 固件记录。
type Firmware struct {
	ID          string    `json:"id"`          // 固件 ID
	Version     string    `json:"version"`     // 版本号
	ChipModel   string    `json:"chipModel"`   // 芯片型号
	DeviceType  string    `json:"deviceType"`  // 设备类型
	Changelog   string    `json:"changelog"`   // 变更日志
	Mandatory   bool      `json:"mandatory"`   // 是否强制更新
	FileName    string    `json:"fileName"`    // 文件名
	FileSize    int64     `json:"fileSize"`    // 文件大小
	Checksum    string    `json:"checksum"`    // SHA-256
	DownloadURL string    `json:"downloadUrl"` // 下载 URL
	CreatedAt   time.Time `json:"createdAt"`   // 创建时间
	UpdatedAt   time.Time `json:"updatedAt"`   // 更新时间
}

// FirmwareFilter 固件查询过滤条件。
type FirmwareFilter struct {
	ChipModel  string `json:"chipModel"`  // 芯片型号过滤
	DeviceType string `json:"deviceType"` // 设备类型过滤
	Keyword    string `json:"keyword"`    // 版本号搜索
	Page       int    `json:"page"`       // 页码
	PageSize   int    `json:"pageSize"`   // 每页条数
}