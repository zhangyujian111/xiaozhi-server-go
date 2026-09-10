package transport

import (
	"context"
	"log/slog"
	"time"

	"github.com/ykt/xiaozhi-server-go/internal/vision"
)

// visionPipeline per-connection 视觉流水线（vision-servo v2）。
//
// 数据流：
//
//	device camera_frame binary (type=0x03)
//	  → wsConn.videoCh
//	  → FrameReceiver (CRC + EXIF GPS 检测 + aisaas DetectFace)
//	  → FaceFollower (跨帧跟踪 + 几何转换 + 死区 + 节流)
//	  → servoCh
//	  → wsConn.SendServo (JSON-RPC 下行到设备)
//
// v1 不下发 FaceProfile（无识别/无 PII）。
//
// 每个 WebSocket 连接独立创建一套 pipeline，互不影响；连接关闭时随 ctx 取消自动退出。
type visionPipeline struct {
	ctx      context.Context
	deviceID string
	handler  *Handler
	receiver *vision.FrameReceiver
	follower *vision.FaceFollower
	servoCh  chan vision.ServoCommand
	logger   *slog.Logger
}

// startVisionPipeline 启动 per-connection 视觉流水线。
// 调用方须在 HandleWebSocket 内、wsConn 创建之后、返回之前调用一次。
// vision 模式未启用时（visionCfg 为空或 Enabled=false）直接返回 no-op。
func (h *Handler) startVisionPipeline(ctx context.Context, ws *wsConn, deviceID string) {
	if h.visionCfg == nil || !h.visionCfg.Enabled {
		return
	}
	if h.visionLogger == nil {
		h.visionLogger = slog.Default()
	}

	imageW := h.visionImageW
	if imageW <= 0 {
		imageW = 640
	}
	imageH := h.visionImageH
	if imageH <= 0 {
		imageH = 480
	}

	// per-conn 通道（v1：仅 servoCh，无 profileCh）
	servoCh := make(chan vision.ServoCommand, 16)

	receiver := vision.NewFrameReceiver(h.visionAisaas, nil, h.visionCfg, h.visionLogger)

	// per-conn FaceFollower（v1：去掉 profileCh 参数）
	followCfg := vision.NewFollowConfig(h.visionCfg, imageW, imageH)
	follower := vision.NewFaceFollower(servoCh, followCfg, imageW, imageH, h.visionLogger)

	// OnDetect → OnDetection 桥接
	receiver.OnDetect(func(detections []vision.Detection) {
		for _, d := range detections {
			follower.OnDetection(d)
		}
	})

	p := &visionPipeline{
		ctx:      ctx,
		deviceID: deviceID,
		handler:  h,
		receiver: receiver,
		follower: follower,
		servoCh:  servoCh,
		logger:   h.visionLogger.With("subsystem", "vision", "device_id", deviceID),
	}

	// 0) 注册 per-conn flush 钩子（vision-servo v2 §4.1）
	h.registerVisionFlushHook(deviceID, func(msg *FlushMessage) {
		follower.Reset()
	})

	// 1) 启动 FrameReceiver（消费 wsConn.videoCh）
	go p.runReceiver(ws.videoCh)

	// 2) 启动下游 drain（servoCh → ws.SendServo + 定期清理跟踪）
	go p.runDownstream(ws)

	// 3) ctx 取消时注销 flush 钩子
	go func() {
		<-ctx.Done()
		h.unregisterVisionFlushHook(deviceID)
	}()

	h.logger.Debug().
		Str("device_id", deviceID).
		Msg("vision pipeline started")
}

// runReceiver 消费视频帧直到 ctx 取消。
func (p *visionPipeline) runReceiver(videoCh <-chan []byte) {
	if err := p.receiver.Run(p.ctx, videoCh, p.deviceID); err != nil {
		p.logger.Error("frame receiver exited with error", "error", err)
	}
	p.logger.Debug("frame receiver stopped")
}

// runDownstream 把舵机命令下发到设备，并定期清理超时跟踪。
//
// v1 范围：仅 servoCh → ws.SendServo；无 profile 下行。
func (p *visionPipeline) runDownstream(ws *wsConn) {
	cleanupTicker := time.NewTicker(1 * time.Second)
	defer cleanupTicker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case cmd, ok := <-p.servoCh:
			if !ok {
				return
			}
			if err := ws.SendServo(cmd.Seq, cmd.PanUs, cmd.TiltUs, cmd.DurationMs); err != nil {
				p.logger.Warn("send servo failed", "seq", cmd.Seq, "error", err)
			}
		case <-cleanupTicker.C:
			p.follower.CleanupLostTracks()
		}
	}
}

// SetCameraFPS 动态调整摄像头帧率（vision-servo v2 §4.2 cam_fps）。
//
// 1. 调用 receiver.SetFPS 同步服务端令牌桶
// 2. 通过 ws.SendCamFps 通知设备
// 3. 失败时记录日志但不中断 pipeline
//
// 使用场景：
//   - 高负载时降帧（reason="high_load"）
//   - 检测置信度高时降帧省电（reason="high_confidence"）
//   - 用户要求恢复（reason="user_request"）
func (p *visionPipeline) SetCameraFPS(fps int, reason string) {
	p.receiver.SetFPS(fps)
	if p.handler != nil && p.deviceID != "" {
		p.handler.mu.RLock()
		conn, ok := p.handler.conns[p.deviceID]
		p.handler.mu.RUnlock()
		if !ok {
			p.logger.Warn("device conn not found for cam_fps", "target_fps", fps)
			return
		}
		if err := conn.SendCamFps(fps, reason); err != nil {
			p.logger.Warn("send cam_fps failed", "target_fps", fps, "error", err)
		}
	}
}
