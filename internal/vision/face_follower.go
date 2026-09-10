package vision

import (
	"log/slog"
	"math"
	"sync"
	"time"
)

// =============================================================================
// FaceFollower 人脸跟踪 + 舵机闭环
// =============================================================================

// FaceFollower 人脸跟踪器。
//
// v1 范围：
//   - 接收 Detection（v1：bbox + confidence + 5 点 keypoints）
//   - 几何转换 + 死区 + 节流 → 输出 ServoCommand
//   - 不再下发 FaceProfile（v1 不做人脸识别）
type FaceFollower struct {
	cmdCh  chan<- ServoCommand
	cfg    *FollowConfig
	logger *slog.Logger

	// 跟踪状态
	mu            sync.RWMutex
	tracks        map[int64]*faceTrackEntry
	lastServoTime map[int64]time.Time
	servoSeq      uint64

	// 图像尺寸（用于几何计算）
	imageW int
	imageH int
}

// NewFaceFollower 创建人脸跟踪器。
func NewFaceFollower(
	cmdCh chan<- ServoCommand,
	cfg *FollowConfig,
	imageW, imageH int,
	logger *slog.Logger,
) *FaceFollower {
	if logger == nil {
		logger = slog.Default()
	}
	return &FaceFollower{
		cmdCh:         cmdCh,
		cfg:           cfg,
		logger:        logger,
		tracks:        make(map[int64]*faceTrackEntry),
		lastServoTime: make(map[int64]time.Time),
		imageW:        imageW,
		imageH:        imageH,
	}
}

// OnDetection 处理检测结果。
func (f *FaceFollower) OnDetection(det Detection) {
	f.mu.Lock()
	defer f.mu.Unlock()

	faceID := det.FaceID
	now := time.Now()

	// 更新跟踪条目（指数移动平均 α=0.3）
	entry, exists := f.tracks[faceID]
	if !exists {
		entry = &faceTrackEntry{
			faceID:          faceID,
			lastSeen:        now,
			smoothedCenterX: float64(det.BoundingBox[0] + det.BoundingBox[2]/2),
			smoothedCenterY: float64(det.BoundingBox[1] + det.BoundingBox[3]/2),
		}
		f.tracks[faceID] = entry
	} else {
		entry.lastSeen = now
		alpha := 0.3
		centerX := float64(det.BoundingBox[0] + det.BoundingBox[2]/2)
		centerY := float64(det.BoundingBox[1] + det.BoundingBox[3]/2)
		entry.smoothedCenterX = alpha*centerX + (1-alpha)*entry.smoothedCenterX
		entry.smoothedCenterY = alpha*centerY + (1-alpha)*entry.smoothedCenterY
	}

	// 几何转换
	panUs, tiltUs, ok := f.geometricConversion(entry.smoothedCenterX, entry.smoothedCenterY)
	if !ok {
		// 死区内，不下发舵机命令
		return
	}

	// 节流检查
	lastTime, hasLast := f.lastServoTime[faceID]
	throttleWindow := time.Duration(f.cfg.FollowGapMs) * time.Millisecond
	if hasLast && now.Sub(lastTime) < throttleWindow {
		// 还在节流窗口内
		return
	}

	// 下发舵机命令
	f.servoSeq++
	cmd := ServoCommand{
		Seq:        f.servoSeq,
		PanUs:      panUs,
		TiltUs:     tiltUs,
		DurationMs: f.cfg.FollowGapMs,
	}

	select {
	case f.cmdCh <- cmd:
		f.lastServoTime[faceID] = now
		f.logger.Debug("servo command sent",
			"face_id", faceID,
			"seq", cmd.Seq,
			"pan_us", panUs,
			"tilt_us", tiltUs,
		)
	default:
		f.logger.Warn("servo command channel full, dropped")
	}
}

// geometricConversion 执行几何转换。
// 将平滑后的人脸中心转换为舵机脉冲宽度。
// 返回 (pan_us, tilt_us, 是否在死区外)。
func (f *FaceFollower) geometricConversion(centerX, centerY float64) (panUs, tiltUs int, ok bool) {
	// 计算偏差（像素）
	errX := centerX - float64(f.imageW)/2.0
	errY := centerY - float64(f.imageH)/2.0

	// 死区检查
	deadZone := float64(f.cfg.DeadZonePx)
	if math.Abs(errX) < deadZone && math.Abs(errY) < deadZone {
		return 0, 0, false
	}

	// 像素偏移 → 角度偏移
	errXDeg := (errX / float64(f.imageW)) * (f.cfg.HFovDeg)
	errYDeg := (errY / float64(f.imageH)) * (f.cfg.VFovDeg)

	// 角度 → 脉冲宽度
	pulseRange := float64(f.cfg.MaxPulseUs - f.cfg.MinPulseUs)
	panRangeDeg := f.cfg.RangePanDeg
	tiltRangeDeg := f.cfg.RangeTiltDeg

	deltaPan := -errXDeg * (panRangeDeg / 180.0) * pulseRange / 2.0
	deltaTilt := errYDeg * (tiltRangeDeg / 180.0) * pulseRange / 2.0

	if f.cfg.InvertPan {
		deltaPan = -deltaPan
	}
	if f.cfg.InvertTilt {
		deltaTilt = -deltaTilt
	}

	targetPan := float64(f.cfg.CenterPanUs) + deltaPan
	targetTilt := float64(f.cfg.CenterTiltUs) + deltaTilt

	// clamp
	panUs = int(clamp(targetPan, float64(f.cfg.MinPulseUs), float64(f.cfg.MaxPulseUs)))
	tiltUs = int(clamp(targetTilt, float64(f.cfg.MinPulseUs), float64(f.cfg.MaxPulseUs)))

	return panUs, tiltUs, true
}

// clamp 将值限制在 [min, max] 范围内。
func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// CleanupLostTracks 清理丢失的跟踪目标。
// 应定期调用（每秒），删除超过 1 秒未出现的 face_id。
func (f *FaceFollower) CleanupLostTracks() {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	for faceID, entry := range f.tracks {
		if now.Sub(entry.lastSeen) > 1*time.Second {
			delete(f.tracks, faceID)
			delete(f.lastServoTime, faceID)
			f.logger.Info("face track lost", "face_id", faceID)
		}
	}
}

// Reset 强制重置所有跟踪状态（vision-servo v2 §4.1 flush）。
//
// 清空：
//   - tracks（所有 face_id 跟踪条目）
//   - lastServoTime（节流时间窗）
//
// 不重置 servoSeq（保持单调递增，避免重放攻击，参见协议 §5.4）。
//
// 设备主动要求重置时调用，常见场景：用户说"重新看"、舵机卡死恢复、长时空闲后恢复。
func (f *FaceFollower) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	clearedTracks := len(f.tracks)
	f.tracks = make(map[int64]*faceTrackEntry)
	f.lastServoTime = make(map[int64]time.Time)

	f.logger.Info("face follower reset",
		"cleared_tracks", clearedTracks,
	)
}

// Stats 返回跟踪统计信息。
func (f *FaceFollower) Stats() (activeTracks int) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.tracks)
}
