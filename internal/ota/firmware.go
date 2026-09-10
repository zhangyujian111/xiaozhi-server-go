package ota

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// firmwareDir 固件文件存储目录（临时目录）。
var firmwareDir string

func init() {
	dir, err := os.MkdirTemp("", "xiaozhi-ota-firmware")
	if err != nil {
		dir = os.TempDir()
	}
	firmwareDir = dir
}

// GenerateFakeFirmwareFile 生成假固件文件（1KB 随机字节），返回文件路径。
//
// 参数：
//   - firmwareID：固件 ID（用于生成唯一文件名）
//
// 返回：文件路径和错误。
//
// 文件命名格式：{firmwareDir}/{firmwareID}.bin
func GenerateFakeFirmwareFile(firmwareID string) (string, error) {
	// 确保目录存在
	if err := os.MkdirAll(firmwareDir, 0755); err != nil {
		return "", fmt.Errorf("ota: create firmware dir: %w", err)
	}

	filename := fmt.Sprintf("%s.bin", firmwareID)
	filePath := filepath.Join(firmwareDir, filename)

	// 检查文件是否已存在
	if _, err := os.Stat(filePath); err == nil {
		return filePath, nil
	}

	// 生成 1KB 随机字节
	data := make([]byte, 1024)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("ota: generate random firmware data: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("ota: write firmware file: %w", err)
	}

	return filePath, nil
}

// FirmwareDir 返回固件文件存储目录。
func FirmwareDir() string {
	return firmwareDir
}

// CleanupFirmwareDir 清理固件文件存储目录。
func CleanupFirmwareDir() error {
	return os.RemoveAll(firmwareDir)
}

// GetFirmwareFilePath 根据固件 ID 获取固件文件路径。
func GetFirmwareFilePath(firmwareID string) string {
	return filepath.Join(firmwareDir, fmt.Sprintf("%s.bin", firmwareID))
}