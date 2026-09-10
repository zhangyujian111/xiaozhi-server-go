package vision

import (
	"context"
	"hash/crc32"
	"sync"
	"testing"
	"time"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
)

// mockAisaasClient 模拟 aisaas 客户端（v1：仅 DetectFace，无 IdentifyFace）。
type mockAisaasClient struct {
	mu       sync.Mutex
	calls    []aisaas.DetectFaceRequest
	response *aisaas.DetectFaceResponse
	err      error
}

func (m *mockAisaasClient) DetectFace(ctx context.Context, req aisaas.DetectFaceRequest) (*aisaas.DetectFaceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestCRCValidation(t *testing.T) {
	cfg := DefaultVisionConfig()
	cfg.CameraFPS = 5

	// 构造正确 CRC 的帧
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	crc := crc32.ChecksumIEEE(jpegData)
	frame := make([]byte, 4+len(jpegData))
	frame[0] = byte(crc >> 24)
	frame[1] = byte(crc >> 16)
	frame[2] = byte(crc >> 8)
	frame[3] = byte(crc)
	copy(frame[4:], jpegData)

	// 验证 CRC 计算正确
	computedCrc := crc32.ChecksumIEEE(frame[4:])
	if computedCrc != crc {
		t.Errorf("CRC mismatch: expected %x, got %x", crc, computedCrc)
	}
}

func TestCRCValidationFails(t *testing.T) {
	cfg := DefaultVisionConfig()
	cfg.CameraFPS = 5

	receiver := NewFrameReceiver((*aisaas.Client)(nil), nil, cfg, nil)
	framesCh := make(chan []byte, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go receiver.Run(ctx, framesCh, "device-001")

	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	wrongCrc := uint32(0xDEADBEEF)
	frame := make([]byte, 4+len(jpegData))
	frame[0] = byte(wrongCrc >> 24)
	frame[1] = byte(wrongCrc >> 16)
	frame[2] = byte(wrongCrc >> 8)
	frame[3] = byte(wrongCrc)
	copy(frame[4:], jpegData)

	framesCh <- frame
	time.Sleep(100 * time.Millisecond)
	cancel()
}

func TestRateLimit(t *testing.T) {
	cfg := DefaultVisionConfig()
	cfg.CameraFPS = 5

	receiver := NewFrameReceiver((*aisaas.Client)(nil), nil, cfg, nil)
	framesCh := make(chan []byte, 100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go receiver.Run(ctx, framesCh, "device-001")

	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	for i := 0; i < 100; i++ {
		crc := crc32.ChecksumIEEE(jpegData)
		frame := make([]byte, 4+len(jpegData))
		frame[0] = byte(crc >> 24)
		frame[1] = byte(crc >> 16)
		frame[2] = byte(crc >> 8)
		frame[3] = byte(crc)
		copy(frame[4:], jpegData)
		framesCh <- frame
	}

	time.Sleep(500 * time.Millisecond)
	cancel()
}

func TestFrameTooShort(t *testing.T) {
	cfg := DefaultVisionConfig()
	receiver := NewFrameReceiver((*aisaas.Client)(nil), nil, cfg, nil)

	framesCh := make(chan []byte, 10)
	_, cancel := context.WithCancel(context.Background())

	framesCh <- []byte{0x01, 0x02}
	cancel()

	_ = receiver
}

func TestOnDetectCallback(t *testing.T) {
	cfg := DefaultVisionConfig()
	cfg.CameraFPS = 5

	receiver := NewFrameReceiver((*aisaas.Client)(nil), nil, cfg, nil)

	var called bool
	receiver.OnDetect(func(detections []Detection) {
		called = true
	})

	_ = called
}

// TestFrameReceiver_SetFPS 验证 SetFPS 调整令牌桶速率并夹紧越界值
// （vision-servo v2 §4.2 cam_fps）。
func TestFrameReceiver_SetFPS(t *testing.T) {
	cfg := DefaultVisionConfig()
	cfg.CameraFPS = 5
	receiver := NewFrameReceiver((*aisaas.Client)(nil), nil, cfg, nil)

	tests := []struct {
		name   string
		input  int
		expect int
	}{
		{"min_clamp_0_to_1", 0, 1},
		{"below_1_to_1", -5, 1},
		{"valid_3", 3, 3},
		{"valid_5", 5, 5},
		{"valid_10", 10, 10},
		{"above_10_clamp", 20, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receiver.SetFPS(tt.input)
			if receiver.cfg.CameraFPS != tt.expect {
				t.Errorf("after SetFPS(%d): cfg.CameraFPS = %d, want %d",
					tt.input, receiver.cfg.CameraFPS, tt.expect)
			}
			if receiver.tokenBucket.rate != float64(tt.expect) {
				t.Errorf("after SetFPS(%d): tokenBucket.rate = %f, want %f",
					tt.input, receiver.tokenBucket.rate, float64(tt.expect))
			}
		})
	}
}
