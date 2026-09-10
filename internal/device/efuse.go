package device

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// ============================================================
// efuse MAC 密钥派生（Q6 决策）
// ============================================================
//
// 安全模型：
//   - AES-256 密钥从 efuse MAC 派生，不存储明文密钥
//   - 使用 SHA-256 哈希 + 固定 salt，确保密钥不可逆推 MAC
//   - dev 环境可从配置文件读取 MAC（cfg.Device.MACID）
//   - 生产环境 MAC 来自设备首次 Hello 握手
//
// 注意：
//   - xiaozhi-server-go 是服务器端，efuse MAC 来自设备上报
//   - 服务器本地不读取 CPU efuse，仅使用设备提供的 MAC 地址派生密钥

// deviceKeySalt 固定 salt，用于 HKDF 风格的密钥派生（简化版 SHA-256）。
const deviceKeySalt = "xiaozhi-server-go-device-key-v1"

// DeriveKeyFromMAC 从 efuse MAC 地址派生 32 字节 AES-256 密钥。
//
// 算法：HKDF-SHA256(normalizedMAC, salt, info)
//   - salt: "xiaozhi-server-go-device-key-v1"
//   - info: "aes-256-gcm-device-key"
//
// 输入：
//   - macAddr: efuse MAC 地址（格式如 "AA:BB:CC:DD:EE:FF" 或 "AABBCCDDEEFF"）
//
// 返回：32 字节 AES-256 密钥
//
// 安全说明：
//   - HKDF 输出固定 32 字节，天然适配 AES-256
//   - salt 为固定常量，防止跨项目密钥重用
//   - MAC 地址变化 → 密钥变化 → 旧加密文件无法解密 → 触发重新注册
func DeriveKeyFromMAC(macAddr string) []byte {
	normalized := normalizeMAC(macAddr)
	ikm := []byte(normalized)
	salt := []byte(deviceKeySalt)
	info := []byte("aes-256-gcm-device-key")
	reader := hkdf.New(sha256.New, ikm, salt, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		// 极端情况：HKDF 不应失败；fallback 到 SHA-256 防止 panic
		h := sha256.Sum256(append(ikm, salt...))
		copy(key, h[:])
	}
	return key
}

// DeriveKeyFromMACWithSalt 从 efuse MAC 和自定义 salt 派生密钥。
//
// 用于需要额外隔离的场景（如不同环境使用不同 salt）。
func DeriveKeyFromMACWithSalt(macAddr string, salt string) []byte {
	normalized := normalizeMAC(macAddr)
	ikm := []byte(normalized)
	saltBytes := []byte(salt)
	info := []byte("aes-256-gcm-device-key")
	reader := hkdf.New(sha256.New, ikm, saltBytes, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		h := sha256.Sum256(append(ikm, saltBytes...))
		copy(key, h[:])
	}
	return key
}

// normalizeMAC 标准化 MAC 地址格式（去除分隔符，统一小写）。
//
// 支持格式：
//   - "AA:BB:CC:DD:EE:FF" → "aabbccddeeff"
//   - "AA-BB-CC-DD-EE-FF" → "aabbccddeeff"
//   - "AABBCCDDEEFF"     → "aabbccddeeff"
//   - "aabb.ccdd.eeff"   → "aabbccddeeff"
func normalizeMAC(macAddr string) string {
	s := strings.ToLower(macAddr)
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// ValidateMAC 校验 MAC 地址格式是否有效。
//
// 有效格式：12 位十六进制字符（去除分隔符后）。
// 返回：标准化后的 MAC 地址，以及是否有效。
func ValidateMAC(macAddr string) (string, error) {
	normalized := normalizeMAC(macAddr)
	if len(normalized) != 12 {
		return "", fmt.Errorf("device: invalid MAC address %q: expected 12 hex chars, got %d", macAddr, len(normalized))
	}
	// 校验是否为十六进制字符
	if _, err := hex.DecodeString(normalized); err != nil {
		return "", fmt.Errorf("device: invalid MAC address %q: not hex", macAddr)
	}
	return normalized, nil
}