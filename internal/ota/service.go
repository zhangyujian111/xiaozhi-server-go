package ota

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// service OTA 固件分发服务实现（内存存储）。
//
// P2 阶段：固件元数据存储在内存 map 中，mock 5 条记录。
// 固件文件生成为本地临时目录的假 .bin 文件（1KB 随机字节）。
// P3 阶段：升级到 aisaas 固件元数据管理 + OSS 签名 URL。
//
// 设备激活状态由 Redis-backed DeviceRegistry 管理（5 分钟激活码 + 永久激活记录）。
type service struct {
	mu         sync.RWMutex
	firmwares  map[string]*Firmware // firmwareID → Firmware
	logger     *slog.Logger
	serverURL  string          // 服务器 URL（用于生成固件下载 URL + WebSocket 地址）
	wsBaseURL  string          // WebSocket 基础 URL（如 ws://host:port）
	wsAuthSalt string          // WebSocket token 签名盐（可选，留空用 uuid）
	registry   *DeviceRegistry // 设备激活注册表（Redis TTL 5min + 永久记录）
}

// Registry 返回内部 DeviceRegistry（供 admin 模块调用）。
func (s *service) Registry() *DeviceRegistry {
	return s.registry
}

// NewService 创建 OTA 固件分发服务。
//
// 参数：
//   - logger：slog 日志器
//   - serverURL：服务器地址（如 "http://localhost:8080"），用于生成固件下载 URL
//   - registry：设备激活注册表（可选，nil 时降级为只生成激活码不持久化）
func NewService(logger *slog.Logger, serverURL string, registry *DeviceRegistry) Service {
	if logger == nil {
		logger = slog.Default()
	}
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	wsBase := strings.Replace(serverURL, "http://", "ws://", 1)
	wsBase = strings.Replace(wsBase, "https://", "wss://", 1)
	svc := &service{
		firmwares:  make(map[string]*Firmware),
		logger:     logger,
		serverURL:  serverURL,
		wsBaseURL:  wsBase,
		wsAuthSalt: "xiaozhi-ws-token-v1",
		registry:   registry,
	}
	// 初始化 mock 固件元数据
	svc.initMockFirmwares()
	return svc
}

// initMockFirmwares 初始化 5 条 mock 固件记录。
func (s *service) initMockFirmwares() {
	now := time.Now()
	mockFirmwares := []Firmware{
		{
			ID:          "fw-esp32s3-v1.0.0",
			Version:     "1.0.0",
			ChipModel:   "ESP32-S3",
			DeviceType:  "esp32",
			Changelog:   "初始版本固件，包含基础语音交互功能。",
			Mandatory:   false,
			FileName:    "xiaozhi-esp32s3-v1.0.0.bin",
			FileSize:    1024,
			Checksum:    "",
			DownloadURL: s.firmwareURL("fw-esp32s3-v1.0.0"),
			CreatedAt:   now.Add(-30 * 24 * time.Hour),
			UpdatedAt:   now.Add(-30 * 24 * time.Hour),
		},
		{
			ID:          "fw-esp32s3-v1.1.0",
			Version:     "1.1.0",
			ChipModel:   "ESP32-S3",
			DeviceType:  "esp32",
			Changelog:   "新增多语言支持，优化唤醒词识别准确率，修复蓝牙连接稳定性问题。",
			Mandatory:   false,
			FileName:    "xiaozhi-esp32s3-v1.1.0.bin",
			FileSize:    1024,
			Checksum:    "",
			DownloadURL: s.firmwareURL("fw-esp32s3-v1.1.0"),
			CreatedAt:   now.Add(-10 * 24 * time.Hour),
			UpdatedAt:   now.Add(-10 * 24 * time.Hour),
		},
		{
			ID:          "fw-esp32c3-v1.0.0",
			Version:     "1.0.0",
			ChipModel:   "ESP32-C3",
			DeviceType:  "esp32",
			Changelog:   "ESP32-C3 芯片初始固件，支持基础语音交互与低功耗模式。",
			Mandatory:   false,
			FileName:    "xiaozhi-esp32c3-v1.0.0.bin",
			FileSize:    1024,
			Checksum:    "",
			DownloadURL: s.firmwareURL("fw-esp32c3-v1.0.0"),
			CreatedAt:   now.Add(-20 * 24 * time.Hour),
			UpdatedAt:   now.Add(-20 * 24 * time.Hour),
		},
		{
			ID:          "fw-esp32-v2.0.0",
			Version:     "2.0.0",
			ChipModel:   "ESP32",
			DeviceType:  "esp32",
			Changelog:   "重大升级：全新架构重构，支持 MQTT 协议，性能提升 30%。",
			Mandatory:   true,
			FileName:    "xiaozhi-esp32-v2.0.0.bin",
			FileSize:    1024,
			Checksum:    "",
			DownloadURL: s.firmwareURL("fw-esp32-v2.0.0"),
			CreatedAt:   now.Add(-5 * 24 * time.Hour),
			UpdatedAt:   now.Add(-5 * 24 * time.Hour),
		},
		{
			ID:          "fw-esp32s3-v1.2.0-beta",
			Version:     "1.2.0-beta",
			ChipModel:   "ESP32-S3",
			DeviceType:  "esp32",
			Changelog:   "Beta 版本：实验性支持本地语音识别（离线 ASR），需要手动开启。",
			Mandatory:   false,
			FileName:    "xiaozhi-esp32s3-v1.2.0-beta.bin",
			FileSize:    1024,
			Checksum:    "",
			DownloadURL: s.firmwareURL("fw-esp32s3-v1.2.0-beta"),
			CreatedAt:   now.Add(-3 * 24 * time.Hour),
			UpdatedAt:   now.Add(-3 * 24 * time.Hour),
		},
	}

	// 为每个 firmware 生成假文件 + 计算 checksum
	for i := range mockFirmwares {
		fw := &mockFirmwares[i]
		filePath, err := GenerateFakeFirmwareFile(fw.ID)
		if err != nil {
			s.logger.Warn("failed to generate fake firmware file",
				"firmware_id", fw.ID,
				"error", err,
			)
			continue
		}
		// 计算 SHA-256
		checksum, err := computeFileSHA256(filePath)
		if err != nil {
			s.logger.Warn("failed to compute firmware checksum",
				"firmware_id", fw.ID,
				"error", err,
			)
		}
		fw.Checksum = checksum
		s.firmwares[fw.ID] = fw
	}

	s.logger.Info("ota mock firmware metadata initialized",
		"count", len(s.firmwares),
		"firmware_dir", firmwareDir,
	)
}

// firmwareURL 生成固件下载 URL。
func (s *service) firmwareURL(firmwareID string) string {
	return fmt.Sprintf("%s/firmware/%s.bin", s.serverURL, firmwareID)
}

// =============================================================================
// Service 接口实现
// =============================================================================

// CheckUpdate 检查固件更新。
func (s *service) CheckUpdate(ctx context.Context, req CheckUpdateReq) (*CheckUpdateResp, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 查找该芯片型号的最新固件
	var latest *Firmware
	for _, fw := range s.firmwares {
		if fw.ChipModel == req.ChipModel {
			if latest == nil || compareVersions(fw.Version, latest.Version) > 0 {
				latest = fw
			}
		}
	}

	if latest == nil {
		return &CheckUpdateResp{
			HasUpdate: false,
		}, nil
	}

	// 版本比较：如果当前版本 >= 最新版本，无需更新
	if req.CurrentVer != "" && compareVersions(req.CurrentVer, latest.Version) >= 0 {
		return &CheckUpdateResp{
			HasUpdate: false,
		}, nil
	}

	s.logger.InfoContext(ctx, "ota update available",
		"device_id", req.DeviceID,
		"current", req.CurrentVer,
		"latest", latest.Version,
		"chip_model", req.ChipModel,
		"mandatory", latest.Mandatory,
	)

	return &CheckUpdateResp{
		HasUpdate:   true,
		FirmwareURL: latest.DownloadURL,
		Version:     latest.Version,
		Mandatory:   latest.Mandatory,
		Changelog:   latest.Changelog,
		Checksum:    latest.Checksum,
		Size:        latest.FileSize,
	}, nil
}

// Activate 查询 OTA 激活状态。
//
// 流程：
//  1. 检查 Redis 中设备是否已激活 → 是：返回 activated=true + WebSocket 信息
//  2. 否则生成新 6 位激活码（存入 Redis TTL 5 分钟）→ 返回 activated=false + ActivationCode
//  3. 总是附带最新固件信息供设备升级判断
func (s *service) Activate(ctx context.Context, req ActivateReq) (*ActivateResp, error) {
	s.logger.InfoContext(ctx, "ota activate request",
		"device_id", req.DeviceID,
		"chip_model", req.ChipModel,
		"version", req.Version,
	)

	now := time.Now()
	resp := &ActivateResp{
		ServerTime: &ServerTimeInfo{
			Timestamp:      now.UnixMilli(),
			TimezoneOffset: 480, // UTC+8
		},
	}

	// 步骤 1：检查设备是否已激活
	if s.registry != nil {
		activated, rec, err := s.registry.IsActivated(ctx, req.DeviceID)
		if err != nil {
			s.logger.WarnContext(ctx, "registry IsActivated error, fallback to code gen",
				"device_id", req.DeviceID, "error", err)
		} else if activated && rec != nil {
			resp.Activated = true
			resp.WebSocket = &WebSocketInfo{
				URL:   fmt.Sprintf("%s/ws/%s", s.wsBaseURL, req.DeviceID),
				Token: s.signWSToken(req.DeviceID),
			}
			s.logger.InfoContext(ctx, "device already activated, returning websocket info",
				"device_id", req.DeviceID,
				"chip_model", req.ChipModel,
				"activated_at", rec.ActivatedAt,
			)
		}
	}

	// 步骤 2：未激活则生成新激活码
	if !resp.Activated {
		code, err := s.GenerateActivationCode(ctx, req.DeviceID, req.DeviceType)
		if err != nil {
			return nil, err
		}
		resp.Activation = &ActivationCode{
			Code:      code.Code,
			Message:   code.Message,
			Challenge: req.DeviceID,
			ExpiresAt: code.ExpiresAt,
		}
		// 存入 Redis TTL 5 分钟
		if s.registry != nil {
			rec := DeviceCodeRecord{
				DeviceID:   req.DeviceID,
				ChipModel:  req.ChipModel,
				Version:    req.Version,
				DeviceType: req.DeviceType,
				IPAddress:  req.IPAddress,
				WiFiSSID:   req.WiFiSSID,
			}
			if err := s.registry.SaveCode(ctx, code.Code, rec); err != nil {
				s.logger.ErrorContext(ctx, "save activation code to redis failed",
					"device_id", req.DeviceID, "code", code.Code, "error", err)
			}
		}
	}

	// 步骤 3：附加最新固件信息（无论是否激活都返回）
	s.mu.RLock()
	var latestFw *Firmware
	for _, fw := range s.firmwares {
		if fw.ChipModel == req.ChipModel {
			if latestFw == nil || compareVersions(fw.Version, latestFw.Version) > 0 {
				latestFw = fw
			}
		}
	}
	s.mu.RUnlock()

	if latestFw != nil {
		resp.Firmware = &FirmwareBrief{
			URL:     latestFw.DownloadURL,
			Version: latestFw.Version,
		}
	}

	// 审计日志
	s.logger.InfoContext(ctx, "ota activation audited",
		"device_id", req.DeviceID,
		"chip_model", req.ChipModel,
		"version", req.Version,
		"activated", resp.Activated,
		"action", "ota.activate",
		"result", "success",
	)

	return resp, nil
}

// signWSToken 为已激活设备生成 WebSocket token（HMAC-SHA256，30 天有效期）。
func (s *service) signWSToken(deviceID string) string {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	mac := hmacSHA256([]byte(s.wsAuthSalt), []byte(fmt.Sprintf("%s|%d", deviceID, exp)))
	return fmt.Sprintf("%d.%s", exp, hex.EncodeToString(mac))
}

// hmacSHA256 计算 HMAC-SHA256。
func hmacSHA256(key, msg []byte) []byte {
	h := sha256.New()
	h.Write(key)
	// simple HMAC (足够测试用；生产建议换 crypto/hmac)
	combined := append(append([]byte{}, key...), msg...)
	h.Reset()
	h.Write(combined)
	return h.Sum(nil)
}

// GenerateActivationCode 为设备生成激活码。
func (s *service) GenerateActivationCode(ctx context.Context, deviceID, deviceType string) (*ActivationCode, error) {
	// 生成 6 位数字激活码
	code := generateActivationCode()
	expiresAt := time.Now().Add(5 * time.Minute)

	s.logger.InfoContext(ctx, "activation code generated",
		"device_id", deviceID,
		"device_type", deviceType,
		"code", code,
		"expires_at", expiresAt,
	)

	return &ActivationCode{
		Code:      code,
		Message:   fmt.Sprintf("请在设备上输入激活码 %s，有效期 5 分钟", code),
		Challenge: deviceID,
		ExpiresAt: expiresAt,
	}, nil
}

// UploadFirmware 上传新固件。
func (s *service) UploadFirmware(ctx context.Context, req UploadFirmwareReq) (*Firmware, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查同一 chipModel + version 组合是否已存在
	for _, fw := range s.firmwares {
		if fw.ChipModel == req.ChipModel && fw.Version == req.Version {
			return nil, ErrFirmwareExists
		}
	}

	now := time.Now()
	fwID := uuid.New().String()

	// 如果提供了文件数据，保存到本地
	checksum := req.Checksum
	if len(req.FileData) > 0 {
		filePath := GetFirmwareFilePath(fwID)
		if err := writeFirmwareFile(filePath, req.FileData); err != nil {
			return nil, fmt.Errorf("ota: write firmware file: %w", err)
		}
		if checksum == "" {
			cs, err := computeFileSHA256(filePath)
			if err != nil {
				return nil, fmt.Errorf("ota: compute checksum: %w", err)
			}
			checksum = cs
		}
	} else {
		// 没有文件数据时生成假文件
		if _, err := GenerateFakeFirmwareFile(fwID); err != nil {
			return nil, err
		}
	}

	fileSize := req.FileSize
	if fileSize == 0 && len(req.FileData) > 0 {
		fileSize = int64(len(req.FileData))
	}

	fw := &Firmware{
		ID:          fwID,
		Version:     req.Version,
		ChipModel:   req.ChipModel,
		DeviceType:  req.DeviceType,
		Changelog:   req.Changelog,
		Mandatory:   req.Mandatory,
		FileName:    req.FileName,
		FileSize:    fileSize,
		Checksum:    checksum,
		DownloadURL: s.firmwareURL(fwID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.firmwares[fwID] = fw

	s.logger.InfoContext(ctx, "firmware uploaded",
		"firmware_id", fwID,
		"version", req.Version,
		"chip_model", req.ChipModel,
		"size", fileSize,
	)

	return fw, nil
}

// ListFirmwares 查询固件列表。
func (s *service) ListFirmwares(ctx context.Context, filter FirmwareFilter) ([]*Firmware, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Firmware, 0)

	for _, fw := range s.firmwares {
		// 芯片型号过滤
		if filter.ChipModel != "" && fw.ChipModel != filter.ChipModel {
			continue
		}
		// 设备类型过滤
		if filter.DeviceType != "" && fw.DeviceType != filter.DeviceType {
			continue
		}
		// 关键词过滤（版本号搜索）
		if filter.Keyword != "" && !containsStr(fw.Version, filter.Keyword) {
			continue
		}

		cp := *fw
		result = append(result, &cp)
	}

	// 简单分页
	if filter.Page > 0 && filter.PageSize > 0 {
		start := (filter.Page - 1) * filter.PageSize
		if start >= len(result) {
			return []*Firmware{}, nil
		}
		end := start + filter.PageSize
		if end > len(result) {
			end = len(result)
		}
		result = result[start:end]
	}

	return result, nil
}

// GetFirmware 获取固件详情。
func (s *service) GetFirmware(ctx context.Context, firmwareID string) (*Firmware, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fw, ok := s.firmwares[firmwareID]
	if !ok {
		return nil, ErrFirmwareNotFound
	}

	cp := *fw
	return &cp, nil
}

// DeleteFirmware 删除固件（软删除：从内存中移除）。
func (s *service) DeleteFirmware(ctx context.Context, firmwareID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.firmwares[firmwareID]; !ok {
		return ErrFirmwareNotFound
	}

	delete(s.firmwares, firmwareID)

	s.logger.InfoContext(ctx, "firmware deleted", "firmware_id", firmwareID)

	return nil
}

// SetMandatory 设置固件为强制更新。
func (s *service) SetMandatory(ctx context.Context, firmwareID string, mandatory bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fw, ok := s.firmwares[firmwareID]
	if !ok {
		return ErrFirmwareNotFound
	}

	fw.Mandatory = mandatory
	fw.UpdatedAt = time.Now()

	s.logger.InfoContext(ctx, "firmware mandatory flag updated",
		"firmware_id", firmwareID,
		"mandatory", mandatory,
	)

	return nil
}

// =============================================================================
// HTTP 处理器（Gin HandlerFunc）
// =============================================================================

// HandleCheckUpdate 处理 GET /api/device/ota?deviceId=xxx&version=yyy
func (s *service) HandleCheckUpdate(c *gin.Context) {
	req := CheckUpdateReq{
		DeviceID:   c.Query("deviceId"),
		ChipModel:  c.Query("chipModel"),
		CurrentVer: c.Query("version"),
		DeviceType: c.Query("deviceType"),
	}

	if req.DeviceID == "" {
		req.DeviceID = c.GetHeader("Device-Id")
	}
	if req.DeviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "deviceId is required (query param or Device-Id header)",
		})
		return
	}

	resp, err := s.CheckUpdate(c.Request.Context(), req)
	if err != nil {
		s.logger.ErrorContext(c.Request.Context(), "ota check update failed",
			"device_id", req.DeviceID,
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// parseActivateReq 解析激活请求，兼容 Java 标准 xiaozhi 固件 schema 与 Go schema。
//
// Java schema: {"mac_address":"AA:BB:...","chip_model_name":"esp32s3",
//               "application":{"version":"2.2.6"},"board":{"ssid":"...","type":"wifi"}}
// Go schema:    {"deviceId":"...","chipModel":"...","deviceType":"...","version":"...","wifiSsid":"..."}
//
// 空 body 也允许（Device-Id header 会作为兜底）。
func parseActivateReq(body []byte) (ActivateReq, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return ActivateReq{}, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ActivateReq{}, err
	}
	req := ActivateReq{}

	// 直接字段（Go schema）
	if s, ok := raw["deviceId"].(string); ok {
		req.DeviceID = s
	} else if s, ok := raw["mac"].(string); ok {
		req.DeviceID = s
	} else if s, ok := raw["mac_address"].(string); ok {
		req.DeviceID = s
	}
	if s, ok := raw["chipModel"].(string); ok {
		req.ChipModel = s
	} else if s, ok := raw["chip_model_name"].(string); ok {
		req.ChipModel = s
	}
	if s, ok := raw["deviceType"].(string); ok {
		req.DeviceType = s
	}
	if s, ok := raw["version"].(string); ok {
		req.Version = s
	} else if app, ok := raw["application"].(map[string]interface{}); ok {
		if s, ok := app["version"].(string); ok {
			req.Version = s
		}
	}
	if s, ok := raw["wifiSsid"].(string); ok {
		req.WiFiSSID = s
	} else if board, ok := raw["board"].(map[string]interface{}); ok {
		if s, ok := board["ssid"].(string); ok {
			req.WiFiSSID = s
		}
		if req.DeviceType == "" {
			if s, ok := board["type"].(string); ok {
				req.DeviceType = s
			}
		}
	}

	return req, nil
}

// HandleActivate 处理 POST /api/device/ota/activate
//
// 兼容两种请求体 schema：
//  1. Go schema: {"deviceId":"...","chipModel":"...","deviceType":"...","version":"...","wifiSsid":"..."}
//  2. Java 标准 xiaozhi 固件 schema: {"mac_address":"...","chip_model_name":"...","application":{"version":"..."},"board":{"ssid":"...","type":"..."}}
func (s *service) HandleActivate(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("read body: %v", err)})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	req, err := parseActivateReq(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	if req.DeviceID == "" {
		req.DeviceID = c.GetHeader("Device-Id")
	}
	if req.DeviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "deviceId is required (body or Device-Id header)",
		})
		return
	}

	// 取客户端 IP（g.ClientIP 自动处理 X-Forwarded-For）
	req.IPAddress = c.ClientIP()

	resp, err := s.Activate(c.Request.Context(), req)
	if err != nil {
		s.logger.ErrorContext(c.Request.Context(), "ota activate failed",
			"device_id", req.DeviceID,
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// HandleFirmwareDownload 处理固件文件下载 GET /firmware/{firmwareId}.bin
func (s *service) HandleFirmwareDownload(c *gin.Context) {
	firmwareID := c.Param("firmwareId")

	// 去除 .bin 后缀（Gin 路由参数会包含扩展名）
	firmwareID = strings.TrimSuffix(firmwareID, ".bin")

	s.mu.RLock()
	_, ok := s.firmwares[firmwareID]
	s.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "firmware not found",
		})
		return
	}

	filePath := GetFirmwareFilePath(firmwareID)

	// 检查文件是否存在
	if _, err := statFile(filePath); err != nil {
		// 尝试重新生成
		var genErr error
		filePath, genErr = GenerateFakeFirmwareFile(firmwareID)
		if genErr != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "firmware file not found",
			})
			return
		}
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.bin\"", firmwareID))
	c.File(filePath)
}

// =============================================================================
// 辅助函数
// =============================================================================

// generateActivationCode 生成 6 位数字激活码。
func generateActivationCode() string {
	b := make([]byte, 3)
	rand.Read(b)
	code := int(b[0])%10*100000 + int(b[1])%10*10000 + int(b[2])%10*1000 +
		int(b[0])%10*100 + int(b[1])%10*10 + int(b[2])%10
	return fmt.Sprintf("%06d", code%1000000)
}

// compareVersions 简单版本号比较。
//
// 支持语义版本（SemVer）格式：major.minor.patch-prerelease
// 返回：>0 表示 v1 > v2，0 表示相等，<0 表示 v1 < v2
func compareVersions(v1, v2 string) int {
	// 简单实现：解析为 major.minor.patch 数字比较
	major1, minor1, patch1 := parseSemVer(v1)
	major2, minor2, patch2 := parseSemVer(v2)

	if major1 != major2 {
		return major1 - major2
	}
	if minor1 != minor2 {
		return minor1 - minor2
	}
	return patch1 - patch2
}

// parseSemVer 解析语义版本号。
func parseSemVer(version string) (major, minor, patch int) {
	// 处理预发布版本（如 "1.2.0-beta"）
	base := version
	for i, c := range version {
		if c == '-' || c == '+' {
			base = version[:i]
			break
		}
	}
	n, err := fmt.Sscanf(base, "%d.%d.%d", &major, &minor, &patch)
	if err != nil || n < 1 {
		// 尝试解析为 "x.y" 格式
		n, err = fmt.Sscanf(base, "%d.%d", &major, &minor)
		if err != nil || n < 1 {
			// 降级到字符串比较
			return 0, 0, 0
		}
	}
	return
}

// computeFileSHA256 计算文件的 SHA-256 校验和。
func computeFileSHA256(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// writeFirmwareFile 写入固件文件到指定路径。
func writeFirmwareFile(filePath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

// statFile 检查文件是否存在。
func statFile(filePath string) (os.FileInfo, error) {
	return os.Stat(filePath)
}

// containsStr 简单子串匹配（不区分大小写）。
func containsStr(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substr[j]
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

// Ensure json import is used for the Activate handler's JSON binding.
var _ = json.Marshal