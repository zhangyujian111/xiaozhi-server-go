// Package transport 提供设备通信协议层抽象（视觉跟踪扩展）。
package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// 错误定义
// =============================================================================

var (
	ErrAuthFail               = errors.New("auth: authentication failed")
	ErrTokenExpired           = errors.New("auth: token expired")
	ErrTokenInvalid           = errors.New("auth: token invalid")
	ErrCapabilityInsufficient = errors.New("auth: capability insufficient")
	ErrPBVerUnsupported       = errors.New("auth: protocol version unsupported")
)

// =============================================================================
// Session Store
// =============================================================================

// SessionStore 会话存储接口。
type SessionStore interface {
	// Put 保存 session token 到期时间。
	Put(deviceID, token string, expiry time.Time)
	// Get 获取 deviceID 对应的 token 和是否有效。
	Get(deviceID string) (token string, ok bool)
	// Delete 删除 deviceID 的 session。
	Delete(deviceID string)
}

// MemorySessionStore 内存会话存储实现。
type MemorySessionStore struct {
	mu      sync.RWMutex
	sessions map[string]sessionEntry
}

type sessionEntry struct {
	token   string
	expiry  time.Time
}

// NewMemorySessionStore 创建内存会话存储。
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]sessionEntry),
	}
}

// Put 保存 session token。
func (s *MemorySessionStore) Put(deviceID, token string, expiry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[deviceID] = sessionEntry{token: token, expiry: expiry}
}

// Get 获取 session token。
func (s *MemorySessionStore) Get(deviceID string) (token string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, exists := s.sessions[deviceID]
	if !exists {
		return "", false
	}
	if time.Now().After(entry.expiry) {
		return "", false
	}
	return entry.token, true
}

// Delete 删除 session。
func (s *MemorySessionStore) Delete(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, deviceID)
}

// CleanExpired 清理过期 session。
func (s *MemorySessionStore) CleanExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	count := 0
	for deviceID, entry := range s.sessions {
		if now.After(entry.expiry) {
			delete(s.sessions, deviceID)
			count++
		}
	}
	return count
}

// =============================================================================
// Token 签名与验证（HMAC-SHA256）
// =============================================================================

// SignToken 生成 HMAC-SHA256 session token。
// 格式：deviceID:ts:nonce:signature
// 返回格式化的字符串。
func SignToken(secret, deviceID, nonce string, ts int64) string {
	data := fmt.Sprintf("%s:%s:%d", deviceID, nonce, ts)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return fmt.Sprintf("%s:%d:%s:%s", deviceID, ts, nonce, hexEncode(h.Sum(nil)))
}

// VerifyToken 验证 session token。
// 返回 true 如果 token 有效且属于 deviceID。
func VerifyToken(token, deviceID, secret string) bool {
	// 解析格式：deviceID:ts:nonce:signature
	parts := strings.Split(token, ":")
	if len(parts) != 4 {
		return false
	}
	id := parts[0]
	tsStr := parts[1]
	nonce := parts[2]

	ts := 0
	if _, err := fmt.Sscanf(tsStr, "%d", &ts); err != nil {
		return false
	}

	if id != deviceID {
		return false
	}
	// 检查时间戳（允许 5 分钟时钟偏移）
	now := time.Now().Unix()
	if now-int64(ts) > 300 {
		return false
	}
	// 重新计算签名
	expected := SignToken(secret, id, nonce, int64(ts))
	// 使用 constant-time 比较防止时序攻击
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

// hexEncode 将字节切片编码为 hex 字符串。
func hexEncode(data []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(data)*2)
	for i, b := range data {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}
	return string(result)
}

// =============================================================================
// Capability 最低要求
// =============================================================================

// MinCapability 定义设备最低 capability 要求。
var MinCapability = struct {
	Camera struct {
		MinWidth  int
		MinHeight int
		MinFPS    int
	}
	Servo struct {
		RequiredChannels []string
	}
}{
	Camera: struct {
		MinWidth  int
		MinHeight int
		MinFPS    int
	}{
		MinWidth:  320,
		MinHeight: 240,
		MinFPS:    1,
	},
	Servo: struct {
		RequiredChannels []string
	}{
		RequiredChannels: []string{"pan", "tilt"},
	},
}

// ValidateCapability 校验设备 capability 是否满足最低要求。
func ValidateCapability(cap *Capability) error {
	if cap.Camera.MaxW < MinCapability.Camera.MinWidth ||
		cap.Camera.MaxH < MinCapability.Camera.MinHeight {
		return fmt.Errorf("%w: camera resolution too low", ErrCapabilityInsufficient)
	}
	if cap.Camera.MaxFPS < MinCapability.Camera.MinFPS {
		return fmt.Errorf("%w: camera fps too low", ErrCapabilityInsufficient)
	}
	// 检查 servo channels 是否包含 pan 和 tilt
	hasPan := false
	hasTilt := false
	for _, ch := range cap.Servo.Channels {
		if ch == "pan" {
			hasPan = true
		}
		if ch == "tilt" {
			hasTilt = true
		}
	}
	if !hasPan || !hasTilt {
		return fmt.Errorf("%w: servo must have pan and tilt channels", ErrCapabilityInsufficient)
	}
	return nil
}

// =============================================================================
// Preshared Token 验证
// =============================================================================

// PresharedTokenStore preshared token 存储接口。
type PresharedTokenStore interface {
	// GetSecret 根据 deviceID 获取对应的 preshared token。
	// 如果 deviceID 不存在，返回 false。
	GetSecret(deviceID string) (token string, ok bool)
}

// MemoryPresharedTokenStore 内存 preshared token 存储。
type MemoryPresharedTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]string // deviceID -> token
}

// NewMemoryPresharedTokenStore 创建内存 preshared token 存储。
func NewMemoryPresharedTokenStore() *MemoryPresharedTokenStore {
	return &MemoryPresharedTokenStore{
		tokens: make(map[string]string),
	}
}

// Put 添加 deviceID 和对应的 token。
func (s *MemoryPresharedTokenStore) Put(deviceID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[deviceID] = token
}

// Get 获取 deviceID 对应的 token。
func (s *MemoryPresharedTokenStore) GetSecret(deviceID string) (token string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok = s.tokens[deviceID]
	return
}

// ValidatePresharedToken 验证 preshared token。
func ValidatePresharedToken(store PresharedTokenStore, deviceID, token string) bool {
	expected, ok := store.GetSecret(deviceID)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}

// =============================================================================
// Hello 认证结果
// =============================================================================

// HelloAuthResult hello 认证结果（内部使用，不用于 JSON 序列化）。
type HelloAuthResult struct {
	OK            bool
	SessionToken  string
	SessionExpiry time.Time
	Negotiated    *Negotiated
	Error         *HelloError
}

// NewHelloAuthResult 创建成功结果。
func NewHelloAuthResult(sessionToken string, sessionExpiry time.Time, negotiated *Negotiated) *HelloAuthResult {
	return &HelloAuthResult{
		OK:            true,
		SessionToken:  sessionToken,
		SessionExpiry: sessionExpiry,
		Negotiated:    negotiated,
	}
}

// NewHelloAuthError 创建错误结果。
func NewHelloAuthError(code, message string) *HelloAuthResult {
	return &HelloAuthResult{
		OK:    false,
		Error: &HelloError{Code: code, Message: message},
	}
}

// =============================================================================
// CRC32 校验（用于 video frame）
// =============================================================================

// CRC32IEEE IEEE CRC32 表。
var CRC32IEEE = crc32MakeTable()

func crc32MakeTable() []uint32 {
	const polynomial uint32 = 0x04C11DB7
	table := make([]uint32, 256)
	for i := range table {
		crc := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ polynomial
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}

// CRC32 计算 CRC32-IEEE 校验和。
func CRC32(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc = (crc << 8) ^ CRC32IEEE[(crc>>24)^uint32(b)]
	}
	return crc ^ 0xFFFFFFFF
}

// VerifyCRC32 验证 CRC32。
// frame 格式：[4B crc BE][N data]
// 返回 data（不含 CRC）和是否有效。
func VerifyCRC32(frame []byte) ([]byte, bool) {
	if len(frame) < 5 {
		return nil, false
	}
	storedCrc := binary.BigEndian.Uint32(frame[0:4])
	data := frame[4:]
	computedCrc := CRC32(data)
	return data, storedCrc == computedCrc
}
