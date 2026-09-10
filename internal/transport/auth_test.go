package transport

import (
	"testing"
	"time"
)

func TestSignVerifyToken(t *testing.T) {
	secret := "test-secret"
	deviceID := "esp32s3-001"
	nonce := "abc123"
	ts := time.Now().Unix()

	// 签发 token
	token := SignToken(secret, deviceID, nonce, ts)
	if token == "" {
		t.Fatal("SignToken returned empty token")
	}

	// 验证 token（使用相同参数）
	if !VerifyToken(token, deviceID, secret) {
		t.Error("VerifyToken failed with correct secret")
	}

	// 使用错误 secret 验证失败
	if VerifyToken(token, deviceID, "wrong-secret") {
		t.Error("VerifyToken should fail with wrong secret")
	}

	// 使用错误 deviceID 验证失败
	if VerifyToken(token, "wrong-device", secret) {
		t.Error("VerifyToken should fail with wrong deviceID")
	}
}

func TestMemorySessionStore(t *testing.T) {
	store := NewMemorySessionStore()
	deviceID := "esp32s3-001"
	token := "session-token-123"
	expiry := time.Now().Add(1 * time.Hour)

	// 初始状态：无 session
	if _, ok := store.Get(deviceID); ok {
		t.Error("expected no session before Put")
	}

	// Put 后可以 Get
	store.Put(deviceID, token, expiry)
	got, ok := store.Get(deviceID)
	if !ok {
		t.Fatal("expected session after Put")
	}
	if got != token {
		t.Errorf("expected token %q, got %q", token, got)
	}

	// Delete 后无 session
	store.Delete(deviceID)
	if _, ok := store.Get(deviceID); ok {
		t.Error("expected no session after Delete")
	}
}

func TestMemorySessionStoreExpiry(t *testing.T) {
	store := NewMemorySessionStore()
	deviceID := "esp32s3-001"
	token := "session-token-123"

	// 放入已过期的 session
	expiredExpiry := time.Now().Add(-1 * time.Second)
	store.Put(deviceID, token, expiredExpiry)

	// 应该无法获取
	if _, ok := store.Get(deviceID); ok {
		t.Error("expected expired session to be unavailable")
	}
}

func TestMemorySessionStoreCleanExpired(t *testing.T) {
	store := NewMemorySessionStore()

	// 放入一个未过期的 session
	store.Put("device-1", "token-1", time.Now().Add(1*time.Hour))
	// 放入一个已过期的 session
	store.Put("device-2", "token-2", time.Now().Add(-1*time.Second))

	// CleanExpired 应该删除 1 个
	count := store.CleanExpired()
	if count != 1 {
		t.Errorf("expected 1 expired entry cleaned, got %d", count)
	}

	// device-1 仍然存在
	if _, ok := store.Get("device-1"); !ok {
		t.Error("expected device-1 session to remain")
	}
	// device-2 已删除
	if _, ok := store.Get("device-2"); ok {
		t.Error("expected device-2 session to be cleaned")
	}
}

func TestValidateCapability(t *testing.T) {
	tests := []struct {
		name    string
		cap     *Capability
		wantErr bool
	}{
		{
			name: "valid capability",
			cap: &Capability{
				Camera: CameraCap{
					MaxW:   640,
					MaxH:   480,
					MaxFPS: 10,
				},
				Servo: ServoCap{
					Channels: []string{"pan", "tilt"},
				},
			},
			wantErr: false,
		},
		{
			name: "camera resolution too low",
			cap: &Capability{
				Camera: CameraCap{
					MaxW:   160,
					MaxH:   120,
					MaxFPS: 5,
				},
				Servo: ServoCap{
					Channels: []string{"pan", "tilt"},
				},
			},
			wantErr: true,
		},
		{
			name: "camera fps too low",
			cap: &Capability{
				Camera: CameraCap{
					MaxW:   640,
					MaxH:   480,
					MaxFPS: 0,
				},
				Servo: ServoCap{
					Channels: []string{"pan", "tilt"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing servo channels",
			cap: &Capability{
				Camera: CameraCap{
					MaxW:   640,
					MaxH:   480,
					MaxFPS: 5,
				},
				Servo: ServoCap{
					Channels: []string{"pan"}, // 缺少 tilt
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCapability(tt.cap)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCapability() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryPresharedTokenStore(t *testing.T) {
	store := NewMemoryPresharedTokenStore()

	// 添加 token
	store.Put("device-1", "token-1")

	// 验证存在
	token, ok := store.GetSecret("device-1")
	if !ok {
		t.Fatal("expected token for device-1")
	}
	if token != "token-1" {
		t.Errorf("expected token-1, got %s", token)
	}

	// 不存在的 device
	_, ok = store.GetSecret("device-999")
	if ok {
		t.Error("expected no token for device-999")
	}
}

func TestValidatePresharedToken(t *testing.T) {
	store := NewMemoryPresharedTokenStore()
	store.Put("device-1", "correct-token")

	if !ValidatePresharedToken(store, "device-1", "correct-token") {
		t.Error("expected valid token to pass")
	}
	if ValidatePresharedToken(store, "device-1", "wrong-token") {
		t.Error("expected wrong token to fail")
	}
	if ValidatePresharedToken(store, "device-999", "any-token") {
		t.Error("expected unknown device to fail")
	}
}
