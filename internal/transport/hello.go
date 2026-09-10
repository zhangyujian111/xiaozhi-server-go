package transport

import (
	"context"
	"fmt"
	"time"
)

// =============================================================================
// Hello 消息处理
// =============================================================================

// HelloHandler 处理 hello 消息的接口。
type HelloHandler interface {
	// Authenticate 验证设备身份。
	// 返回 session token 和过期时间，如果验证失败返回 error。
	Authenticate(ctx context.Context, deviceID, presharedToken string) (sessionToken string, expiry time.Time, err error)
}

// DefaultHelloHandler 默认 hello 消息处理器。
type DefaultHelloHandler struct {
	presharedStore PresharedTokenStore
	sessionStore   SessionStore
	sessionSecret  string
	sessionTTL     time.Duration
	negotiator     *HelloNegotiator
}

// NewDefaultHelloHandler 创建默认 hello 处理器。
func NewDefaultHelloHandler(
	presharedStore PresharedTokenStore,
	sessionStore SessionStore,
	sessionSecret string,
	sessionTTL time.Duration,
	negotiator *HelloNegotiator,
) *DefaultHelloHandler {
	return &DefaultHelloHandler{
		presharedStore: presharedStore,
		sessionStore:   sessionStore,
		sessionSecret:  sessionSecret,
		sessionTTL:     sessionTTL,
		negotiator:     negotiator,
	}
}

// Authenticate 验证设备 preshared token 并签发 session token。
func (h *DefaultHelloHandler) Authenticate(ctx context.Context, deviceID, presharedToken string) (string, time.Time, error) {
	// 验证 preshared token
	if !ValidatePresharedToken(h.presharedStore, deviceID, presharedToken) {
		return "", time.Time{}, fmt.Errorf("%w: invalid token", ErrAuthFail)
	}

	// 生成 session token
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())
	ts := time.Now().Unix()
	sessionToken := SignToken(h.sessionSecret, deviceID, nonce, ts)
	expiry := time.Now().Add(h.sessionTTL)

	// 保存到 session store
	h.sessionStore.Put(deviceID, sessionToken, expiry)

	return sessionToken, expiry, nil
}

// =============================================================================
// Hello 协商器
// =============================================================================

// HelloNegotiator hello 参数协商器。
type HelloNegotiator struct {
	defaultFPS   int
	defaultGapMs int
	defaultDeadZone int
}

// NewHelloNegotiator 创建 hello 协商器。
func NewHelloNegotiator(defaultFPS, defaultGapMs, defaultDeadZone int) *HelloNegotiator {
	return &HelloNegotiator{
		defaultFPS:     defaultFPS,
		defaultGapMs:   defaultGapMs,
		defaultDeadZone: defaultDeadZone,
	}
}

// Negotiate 协商视觉跟踪参数。
// 根据设备 capability 和服务端配置，返回最终协商值。
func (n *HelloNegotiator) Negotiate(cap *Capability) Negotiated {
	// 使用服务端的配置值（强约束），设备必须采用
	return Negotiated{
		CameraFPS:   n.defaultFPS,
		FollowGapMs: n.defaultGapMs,
		DeadZonePx:  n.defaultDeadZone,
	}
}

// =============================================================================
// Hello 请求验证与响应构建
// =============================================================================

// ValidateHelloRequest 验证 hello 请求。
func ValidateHelloRequest(r *HelloRequest) error {
	if r.DeviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if r.PBVer != 2 {
		return fmt.Errorf("%w: required pb_ver=2, got %d", ErrPBVerUnsupported, r.PBVer)
	}
	if r.Token == "" {
		return fmt.Errorf("%w: token is required", ErrAuthFail)
	}
	if r.Capability == nil {
		return fmt.Errorf("%w: capability is required", ErrCapabilityInsufficient)
	}
	if err := ValidateCapability(r.Capability); err != nil {
		return err
	}
	return nil
}

// BuildHelloAckMsg 从认证结果构建 hello ACK 消息。
func BuildHelloAckMsg(authResult *HelloAuthResult) *HelloAckMsg {
	if !authResult.OK {
		return &HelloAckMsg{
			OK:    false,
			Error: authResult.Error,
		}
	}
	negotiated := authResult.Negotiated
	if negotiated == nil {
		negotiated = &Negotiated{}
	}
	return &HelloAckMsg{
		OK:               true,
		SessionToken:     authResult.SessionToken,
		SessionExpiresAt: authResult.SessionExpiry.Format(time.RFC3339),
		Negotiated:       *negotiated,
	}
}
