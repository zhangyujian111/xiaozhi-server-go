package device

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
)

// ============================================================
// KeyStore — AES-256-GCM 加密本地存储
// ============================================================
//
// 安全模型（Q6 决策）：
//   - AES-256-GCM 加密整个 JSON 文件
//   - 每次加密生成随机 nonce（12 字节），保证相同明文产生不同密文
//   - 文件格式：nonce (12B) + ciphertext + tag (16B)
//   - 原子写入：write .tmp → fsync → rename（防止文件损坏）
//   - 文件权限：0600（仅 owner 可读写）
//   - AES-256 密钥从 efuse MAC 派生（SHA-256 + 固定 salt），不存储明文密钥
//
// 文件位置：configs/device.key.enc（由 Config.EncryptedKeyFile 配置）

// KeyStore 管理设备 API Key 的 AES-256-GCM 加密存储。
type KeyStore struct {
	filePath  string       // 加密文件路径
	aesgcm    cipher.AEAD  // AES-GCM cipher 实例（预初始化，线程安全）
	logger    *slog.Logger // 日志器
}

// deriveKeyFromMAC 从 efuse MAC 地址派生 32 字节 AES-256 密钥。
//
// 算法：HKDF-SHA256(macAddr, salt, info)
//   - salt: "xiaozhi-server-go-device-key-v1"
//   - info: "aes-256-gcm-device-key"
// salt 为固定常量，防止跨项目密钥重用。
// 注：efuse.go 中 DeriveKeyFromMAC 为导出版本，此处为内部使用。
func deriveKeyFromMAC(macAddr string) []byte {
	ikm := []byte(macAddr)
	salt := []byte("xiaozhi-server-go-device-key-v1")
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

// NewKeyStore 创建 KeyStore 实例。
//
// 参数：
//   - filePath: 加密文件路径（如 configs/device.key.enc）
//   - efuseMAC: efuse MAC 地址（用于 AES-256 密钥派生）
//   - logger: slog 日志器（nil 则使用默认）
//
// 返回：KeyStore 实例，或密钥无效时返回 error
func NewKeyStore(filePath string, efuseMAC string, logger *slog.Logger) (*KeyStore, error) {
	key := deriveKeyFromMAC(efuseMAC)
	if len(key) != 32 {
		return nil, fmt.Errorf("device: derived key length %d, expected 32", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("device: create AES cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("device: create GCM: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &KeyStore{
		filePath: filePath,
		aesgcm:   aesgcm,
		logger:   logger,
	}, nil
}

// Exists 检查加密文件是否存在。
func (ks *KeyStore) Exists() bool {
	_, err := os.Stat(ks.filePath)
	return err == nil
}

// Save 加密并保存 Credentials 到文件。
//
// 流程：
//  1. JSON 序列化 Credentials
//  2. AES-256-GCM 加密（随机 nonce + 加密 + tag）
//  3. 原子写入：write .tmp → fsync → rename
//
// 参数：
//   - creds: 设备 API Key 凭证（含明文 APIKey）
func (ks *KeyStore) Save(creds *Credentials) error {
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("device: marshal creds: %w", err)
	}

	nonce := make([]byte, ks.aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("device: generate nonce: %w", err)
	}

	// Seal 格式：nonce (12B) + encrypted_data + tag (16B)
	// 注意：nonce 作为 Seal 的第一个参数（dst），使得 nonce 在密文前面
	ciphertext := ks.aesgcm.Seal(nonce, nonce, plaintext, nil)

	if err := ks.atomicWrite(ciphertext); err != nil {
		return fmt.Errorf("device: write key file: %w", err)
	}

	ks.logger.Info("device: key file saved",
		"path", ks.filePath,
		"deviceId", creds.DeviceID,
		"keyId", creds.KeyID,
	)
	return nil
}

// Load 从加密文件加载并解密 Credentials。
//
// 返回：
//   - *Credentials: 解密后的凭证（含明文 APIKey）
//   - error: 文件不存在、解密失败、JSON 解析失败时返回错误
//
// 错误处理：
//   - 文件不存在 → 返回 ErrKeyFileNotFound（调用方应触发首次注册）
//   - 解密失败 → 返回 ErrKeyFileCorrupted（调用方应触发重新注册）
//   - JSON 解析失败 → 返回 ErrKeyFileCorrupted
func (ks *KeyStore) Load() (*Credentials, error) {
	data, err := os.ReadFile(ks.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("device: %w: %s", ErrKeyFileNotFound, ks.filePath)
		}
		return nil, fmt.Errorf("device: read key file %s: %w", ks.filePath, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("device: %w: empty file %s", ErrKeyFileCorrupted, ks.filePath)
	}

	nonceSize := ks.aesgcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("device: %w: ciphertext too short (%d bytes, need at least %d)", ErrKeyFileCorrupted, len(data), nonceSize)
	}

	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := ks.aesgcm.Open(nil, nonce, ct, nil)
	if err != nil {
		ks.logger.Warn("device: key file decryption failed",
			"path", ks.filePath,
			"error", err,
		)
		return nil, fmt.Errorf("device: %w: decrypt: %s", ErrKeyFileCorrupted, err.Error())
	}

	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return nil, fmt.Errorf("device: %w: parse json: %s", ErrKeyFileCorrupted, err.Error())
	}

	ks.logger.Info("device: key file loaded",
		"path", ks.filePath,
		"deviceId", creds.DeviceID,
		"keyId", creds.KeyID,
	)
	return &creds, nil
}

// Delete 删除加密文件（用于清理或重新注册）。
func (ks *KeyStore) Delete() error {
	if err := os.Remove(ks.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("device: delete key file: %w", err)
	}
	ks.logger.Warn("device: key file deleted", "path", ks.filePath)
	return nil
}

// FilePath 返回加密文件路径。
func (ks *KeyStore) FilePath() string {
	return ks.filePath
}

// atomicWrite 原子写入文件（write .tmp → fsync → rename）。
//
// 防止以下场景导致文件损坏：
//   - 进程崩溃（写入中途）
//   - 磁盘满（部分写入）
//   - 并发写入（虽然有 manager 锁保护，但作为防御层）
func (ks *KeyStore) atomicWrite(data []byte) error {
	dir := filepath.Dir(ks.filePath)
	tmpFile := ks.filePath + ".tmp"

	// 确保目录存在
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	// 写入临时文件
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	n, err := f.Write(data)
	if err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("write temp file: %w", err)
	}
	if n != len(data) {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("short write: %d/%d bytes", n, len(data))
	}

	// fsync 确保数据落盘
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("fsync temp file: %w", err)
	}
	f.Close()

	// 原子 rename
	if err := os.Rename(tmpFile, ks.filePath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}