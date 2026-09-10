package vision

import (
	"context"
	"hash/crc32"
	"log/slog"
	"sync"
	"time"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
)

// FrameReceiver 视频帧接收器。
// 从 framesCh 消费原始 JPEG 帧，进行校验和处理后调用 aisaas 人脸检测（v1：detect + 5 点 keypoints，无识别）。
type FrameReceiver struct {
	aisaasClient   *aisaas.Client
	metrics        *VisionMetrics
	cfg            *VisionConfig
	logger         *slog.Logger
	tokenBucket    *tokenBucket
	onDetect       OnDetectCallback
	onDetectOnce   sync.Once
}

// OnDetectCallback 检测结果回调（v1：携带 bbox + 5 点 keypoints，无 PII）。
type OnDetectCallback func(detections []Detection)

// NewFrameReceiver 创建帧接收器。
func NewFrameReceiver(
	aisaasClient *aisaas.Client,
	metrics *VisionMetrics,
	cfg *VisionConfig,
	logger *slog.Logger,
) *FrameReceiver {
	if logger == nil {
		logger = slog.Default()
	}
	fps := cfg.CameraFPS
	if fps <= 0 {
		fps = 5
	}
	return &FrameReceiver{
		aisaasClient: aisaasClient,
		metrics:      metrics,
		cfg:          cfg,
		logger:       logger,
		tokenBucket:  newTokenBucket(fps),
	}
}

// OnDetect 设置检测结果回调。
func (r *FrameReceiver) OnDetect(cb OnDetectCallback) {
	r.onDetectOnce.Do(func() {
		r.onDetect = cb
	})
}

// SetFPS 动态调整令牌桶速率（vision-servo v2 §4.2 cam_fps）。
//
// 服务端在向设备发送 cam_fps 指令后应同步调用本方法，保证两侧速率一致。
// fps 范围 1-10；越界值会被夹紧。
func (r *FrameReceiver) SetFPS(fps int) {
	if fps < 1 {
		fps = 1
	}
	if fps > 10 {
		fps = 10
	}
	r.tokenBucket.SetRate(float64(fps))
	r.cfg.CameraFPS = fps
	r.logger.Info("frame receiver fps updated", "new_fps", fps)
}

// Run 启动帧接收循环。
// 从 framesCh 消费原始帧（格式：[4B crc BE][N jpeg]），进行 CRC 校验、
// EXIF GPS 检测，然后调用 aisaas.DetectFace。
// 错误不致命，记录后继续运行。
func (r *FrameReceiver) Run(ctx context.Context, framesCh <-chan []byte, deviceID string) error {
	r.logger.InfoContext(ctx, "frame receiver started",
		"device_id", deviceID,
		"fps", r.cfg.CameraFPS,
	)
	for {
		select {
		case <-ctx.Done():
			r.logger.InfoContext(ctx, "frame receiver stopped", "device_id", deviceID)
			return nil
		case frame, ok := <-framesCh:
			if !ok {
				r.logger.InfoContext(ctx, "frames channel closed", "device_id", deviceID)
				return nil
			}
			r.processFrame(ctx, frame, deviceID)
		}
	}
}

// processFrame 处理单帧。
func (r *FrameReceiver) processFrame(ctx context.Context, frame []byte, deviceID string) {
	start := time.Now()

	// 1. 令牌桶限速
	if !r.tokenBucket.Allow() {
		if r.metrics != nil {
			r.metrics.RecordCameraFrame(deviceID, string(FrameStatusDropped), 0)
		}
		r.logger.DebugContext(ctx, "frame rate limited", "device_id", deviceID)
		return
	}

	// 2. CRC 校验（frame 格式：[4B crc BE][N jpeg]）
	if len(frame) < 5 {
		r.recordDropped(deviceID, "frame too short")
		return
	}
	storedCrc := uint32(frame[0])<<24 | uint32(frame[1])<<16 | uint32(frame[2])<<8 | uint32(frame[3])
	jpegData := frame[4:]
	computedCrc := crc32.ChecksumIEEE(jpegData)
	if storedCrc != computedCrc {
		r.recordDropped(deviceID, "crc mismatch")
		return
	}

	// 3. EXIF GPS 检测
	if hasGPSEXIF(jpegData) {
		r.recordError(deviceID, "exif gps detected")
		return
	}

	// 4. 调用 aisaas detect（v1：仅检测 + 5 点 keypoints）
	if r.aisaasClient == nil {
		r.logger.DebugContext(ctx, "aisaas client not configured, skipping detect")
		return
	}
	tsMs := time.Now().UnixMilli()
	resp, err := r.aisaasClient.DetectFace(ctx, aisaas.DetectFaceRequest{
		DeviceID:  deviceID,
		ImageData: jpegData,
		FrameCRC:  storedCrc,
		TsMs:      tsMs,
	})

	duration := time.Since(start).Seconds()
	if r.metrics != nil {
		r.metrics.RecordCameraFrame(deviceID, string(FrameStatusSuccess), duration)
	}

	if err != nil {
		r.logger.WarnContext(ctx, "detect face failed",
			"device_id", deviceID,
			"error", err,
		)
		if r.metrics != nil {
			r.metrics.RecordFaceDetect(deviceID, "error")
		}
		return
	}

	if r.metrics != nil {
		status := "miss"
		if resp.Hit {
			status = "hit"
		}
		r.metrics.RecordFaceDetect(deviceID, status)
	}

	// 5. 回调通知（携带 bbox + 5 点 keypoints，无任何 PII）
	if r.onDetect != nil && len(resp.Detections) > 0 {
		detections := make([]Detection, len(resp.Detections))
		for i, d := range resp.Detections {
			var kps [5]Keypoint
			for j := 0; j < 5; j++ {
				kps[j] = Keypoint{X: d.Keypoints[j].X, Y: d.Keypoints[j].Y}
			}
			detections[i] = Detection{
				FaceID:      d.FaceID,
				BoundingBox: d.BoundingBox,
				Confidence:  d.Confidence,
				Keypoints:   kps,
				TsMs:        tsMs,
			}
		}
		r.onDetect(detections)
	}

	r.logger.DebugContext(ctx, "frame detected",
		"device_id", deviceID,
		"detections", len(resp.Detections),
		"latency_ms", resp.LatencyMs,
	)
}

func (r *FrameReceiver) recordDropped(deviceID, reason string) {
	if r.metrics != nil {
		r.metrics.RecordCameraFrame(deviceID, string(FrameStatusDropped), 0)
	}
	r.logger.DebugContext(context.Background(), "frame dropped",
		"device_id", deviceID,
		"reason", reason,
	)
}

func (r *FrameReceiver) recordError(deviceID, reason string) {
	if r.metrics != nil {
		r.metrics.RecordCameraFrame(deviceID, string(FrameStatusError), 0)
	}
	r.logger.DebugContext(context.Background(), "frame error",
		"device_id", deviceID,
		"reason", reason,
	)
}

// =============================================================================
// 令牌桶（帧率限制）
// =============================================================================

type tokenBucket struct {
	rate       float64
	capacity   float64
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

func newTokenBucket(fps int) *tokenBucket {
	return &tokenBucket{
		rate:       float64(fps),
		capacity:   float64(fps),
		tokens:     float64(fps),
		lastRefill: time.Now(),
	}
}

func (tb *tokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now

	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// SetRate 动态调整令牌桶速率。
//
// 调整后 capacity 同步变更以保持 1 秒缓冲；不会清空已有令牌。
func (tb *tokenBucket) SetRate(newRate float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.rate = newRate
	tb.capacity = newRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
}

// =============================================================================
// EXIF GPS 检测（二进制扫描）
// =============================================================================

// hasGPSEXIF 检测 JPEG 是否包含 GPS EXIF 信息。
func hasGPSEXIF(jpeg []byte) bool {
	for i := 0; i < len(jpeg)-4; i++ {
		if jpeg[i] == 0xFF && jpeg[i+1] == 0xE1 {
			end := i + 4 + 100
			if end > len(jpeg) {
				end = len(jpeg)
			}
			for j := i + 4; j < end-1; j++ {
				if jpeg[j] == 0x88 && jpeg[j+1] == 0x25 {
					return true
				}
			}
			return false
		}
	}
	return false
}
