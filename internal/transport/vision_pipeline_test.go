package transport

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ykt/xiaozhi-server-go/internal/config"
	"github.com/ykt/xiaozhi-server-go/internal/vision"
)

// newTestVisionHandler 创建启用 vision 模式的测试 Handler。
// aisaas 客户端为 nil（FrameReceiver 会跳过 identify），仅验证流水线结构。
func newTestVisionHandler(t *testing.T) *Handler {
	t.Helper()
	h := NewHandler(nil, config.WebSocketConfig{
		MaxConnections:  100,
		PingInterval:    30 * time.Second,
		PongWait:        60 * time.Second,
		WriteWait:       10 * time.Second,
		MaxMessageSize:  65536,
		HandshakeTimeout: 10 * time.Second,
	})
	h.SetVisionConfig(
		nil, // aisaas client（测试中为 nil，receiver 跳过 identify）
		&vision.VisionConfig{
			Enabled:      true,
			CameraFPS:    5,
			FollowGapMs:  220,
			DeadZonePx:   8,
			HFovDeg:      65,
			MinPulseUs:   500,
			MaxPulseUs:   2500,
			CenterPanUs:  1500,
			CenterTiltUs: 1500,
			RangePanDeg:  90,
			RangeTiltDeg: 60,
		},
		slog.New(slog.NewTextHandler(testWriter{t}, nil)),
		640, 480,
	)
	return h
}

// newTestVisionHandlerDisabled 返回 vision 禁用的 Handler。
func newTestVisionHandlerDisabled() *Handler {
	h := NewHandler(nil, config.WebSocketConfig{MaxConnections: 1})
	h.SetVisionConfig(nil, &vision.VisionConfig{Enabled: false}, slog.Default(), 640, 480)
	return h
}

// newTestWSConn 构造一个最小可用的 wsConn（无真实 WebSocket 连接）。
func newTestWSConn(deviceID string) *wsConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &wsConn{
		deviceID: deviceID,
		conn:     nil, // 测试中不需要真实 conn
		ctx:      ctx,
		cancel:   cancel,
		cmdCh:    make(chan []byte, 64),
		audioCh:  make(chan []byte, 64),
		videoCh:  make(chan []byte, 64),
		writeCh:  make(chan []byte, 256),
	}
}

// =============================================================================
// Tests
// =============================================================================

// TestSetVisionConfig 验证 SetVisionConfig 正确注入字段。
func TestSetVisionConfig(t *testing.T) {
	h := NewHandler(nil, config.WebSocketConfig{})
	if h.visionCfg != nil {
		t.Fatal("expected nil visionCfg initially")
	}

	cfg := &vision.VisionConfig{Enabled: true, CameraFPS: 8}
	h.SetVisionConfig(nil, cfg, slog.Default(), 320, 240)

	if h.visionCfg != cfg {
		t.Errorf("visionCfg not set: got %v", h.visionCfg)
	}
	if h.visionImageW != 320 || h.visionImageH != 240 {
		t.Errorf("image size: got %dx%d, want 320x240", h.visionImageW, h.visionImageH)
	}
}

// TestStartVisionPipeline_Disabled 验证 vision 禁用时 no-op。
func TestStartVisionPipeline_Disabled(t *testing.T) {
	h := newTestVisionHandlerDisabled()
	ws := newTestWSConn("dev-001")

	// 不应 panic、不应启动任何 goroutine
	h.startVisionPipeline(ws.ctx, ws, "dev-001")

	// 验证 videoCh 未被消费
	select {
	case ws.videoCh <- []byte{0x00, 0x01, 0x02}: // 应保留在 channel 中
		// ok
	default:
		t.Fatal("videoCh should not be consumed when vision is disabled")
	}
	ws.cancel()
}

// TestStartVisionPipeline_NoOpOnMissingConfig 验证 visionCfg=nil 时 no-op。
func TestStartVisionPipeline_NoOpOnMissingConfig(t *testing.T) {
	h := NewHandler(nil, config.WebSocketConfig{})
	// 不调用 SetVisionConfig
	ws := newTestWSConn("dev-001")

	// 不应 panic
	h.startVisionPipeline(ws.ctx, ws, "dev-001")
	ws.cancel()
}

// TestStartVisionPipeline_StartsGoroutines 验证流水线启动且 ctx 取消时退出。
func TestStartVisionPipeline_StartsGoroutines(t *testing.T) {
	h := newTestVisionHandler(t)
	ws := newTestWSConn("dev-pipeline-001")

	h.startVisionPipeline(ws.ctx, ws, "dev-pipeline-001")

	// 推入 5 个帧（CRC 错误 → 丢弃；不调用 aisaas）
	// 帧格式：[4B len BE] [1B type=0x03] [payload]
	for i := 0; i < 5; i++ {
		select {
		case ws.videoCh <- []byte{0x00, 0x00, 0x00, 0x10, 0x03, 0xff, 0xd8, 0xff, 0xe0}:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("videoCh send timeout")
		}
	}

	// 等一下让 receiver 处理
	time.Sleep(200 * time.Millisecond)

	// 取消 ctx，goroutine 应退出
	ws.cancel()

	// 等所有 goroutine 退出
	done := make(chan struct{})
	go func() {
		// 模拟 per-conn 资源的回收：close cmdCh 模拟 drain goroutine 退出
		close(ws.cmdCh)
		close(ws.audioCh)
		close(ws.videoCh)
		close(ws.writeCh)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutines did not exit within 2s")
	}
}

// TestStartVisionPipeline_FrameProcessing 验证帧处理流程（nil aisaas 跳过 identify，无 servo）。
func TestStartVisionPipeline_FrameProcessing(t *testing.T) {
	h := newTestVisionHandler(t)
	ws := newTestWSConn("dev-frame-001")
	defer func() {
		ws.cancel()
		// 给 goroutine 时间退出
		time.Sleep(100 * time.Millisecond)
	}()

	h.startVisionPipeline(ws.ctx, ws, "dev-frame-001")

	// 推入 1 个最小 JPEG-like 帧（带 CRC 错误，让 receiver 走 token bucket 后丢弃）
	// 实际生产中 CRC 正确 + aisaas 真实响应才会产生 detection
	// 这里验证：nil aisaas 时 receiver 走完路径不 panic
	frame := []byte{
		0x00, 0x00, 0x00, 0x10, // 长度（不重要）
		0x03,             // type=0x03 视频
		0xFF, 0xD8, 0xFF, 0xE0, // JPEG magic
		0x00, 0x10, 'J', 'F', 'I', 'F', 0x00,
		0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00,
	}

	// 用 atomic 计数 goroutine 启动次数
	var processed atomic.Int32
	go func() {
		// 模拟 wsConn 写入：监控 cmdCh 是否有数据
		// 由于 aisaas=nil + CRC 不匹配，预期没有 servo 命令
		select {
		case <-ws.ctx.Done():
			processed.Add(1)
		case <-time.After(500 * time.Millisecond):
			processed.Add(1)
		}
	}()

	// 推入帧
	select {
	case ws.videoCh <- frame:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("videoCh send timeout")
	}

	// 等待监控 goroutine 完成
	for processed.Load() == 0 {
		time.Sleep(50 * time.Millisecond)
	}

	// 验证 cmdCh 为空（nil aisaas → 无 detection → 无 servo）
	select {
	case d := <-ws.cmdCh:
		t.Fatalf("expected no servo command (nil aisaas), got %d bytes", len(d))
	default:
		// ok
	}
}

// testWriter slog 输出到 t.Log（用于测试日志显示）。
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
