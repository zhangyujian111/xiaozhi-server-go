// Package vision 提供视觉跟踪功能（人脸检测 + 关键点 + 舵机闭环）。
package vision

import (
	"math"
	"time"
)

// =============================================================================
// 公共类型
// =============================================================================

// Keypoint 5 点人脸关键点（按 YuNet 标准顺序）
//   0: 左眼   1: 右眼   2: 鼻尖   3: 左嘴角   4: 右嘴角
type Keypoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Detection 人脸检测结果（v1：detect + 5 点 keypoints，无识别/无 PII）。
type Detection struct {
	FaceID       int64        `json:"faceId"`
	BoundingBox  [4]int       `json:"boundingBox"` // [x, y, w, h]
	Confidence   float64      `json:"confidence"`
	Keypoints    [5]Keypoint  `json:"keypoints"` // 5 点 landmarks
	ImageCenterX int          `json:"imageCenterX"`
	ImageCenterY int          `json:"imageCenterY"`
	ImageW       int          `json:"imageW"`
	ImageH       int          `json:"imageH"`
	TsMs         int64        `json:"tsMs"`
}

// ServoCommand 舵机控制命令。
type ServoCommand struct {
	Seq        uint64 `json:"seq"`
	PanUs      int    `json:"panUs"`
	TiltUs     int    `json:"tiltUs"`
	DurationMs int    `json:"durationMs"`
}

// FrameStatus 帧处理状态。
type FrameStatus string

const (
	FrameStatusSuccess  FrameStatus = "success"
	FrameStatusDropped  FrameStatus = "dropped"
	FrameStatusError    FrameStatus = "error"
)

// =============================================================================
// 跟踪状态
// =============================================================================

// faceTrackEntry 跨帧跟踪条目。
type faceTrackEntry struct {
	faceID          int64
	lastSeen        time.Time
	smoothedCenterX float64 // 指数移动平均
	smoothedCenterY float64
}

// =============================================================================
// 配置
// =============================================================================

// VisionConfig 视觉跟踪配置。
type VisionConfig struct {
	Enabled      bool    `mapstructure:"enabled"`
	CameraFPS    int     `mapstructure:"camera_fps"`     // 默认 5
	FollowGapMs  int     `mapstructure:"follow_gap_ms"`  // 220
	DeadZonePx   int     `mapstructure:"dead_zone_px"`    // 8 (范围 8-20)
	HFovDeg      float64 `mapstructure:"hfov_deg"`       // 65
	MinPulseUs   int     `mapstructure:"min_pulse_us"`   // 500
	MaxPulseUs   int     `mapstructure:"max_pulse_us"`   // 2500
	CenterPanUs  int     `mapstructure:"center_pan_us"`  // 1500
	CenterTiltUs int     `mapstructure:"center_tilt_us"` // 1500
	RangePanDeg  float64 `mapstructure:"range_pan_deg"`  // 90
	RangeTiltDeg float64 `mapstructure:"range_tilt_deg"` // 60
	InvertPan    bool    `mapstructure:"invert_pan"`
	InvertTilt   bool    `mapstructure:"invert_tilt"`
}

// DefaultVisionConfig 返回默认配置。
func DefaultVisionConfig() *VisionConfig {
	return &VisionConfig{
		Enabled:      true,
		CameraFPS:    5,
		FollowGapMs:  220,
		DeadZonePx:   8,
		HFovDeg:      65.0,
		MinPulseUs:   500,
		MaxPulseUs:   2500,
		CenterPanUs:  1500,
		CenterTiltUs: 1500,
		RangePanDeg:  90.0,
		RangeTiltDeg: 60.0,
		InvertPan:    false,
		InvertTilt:   false,
	}
}

// Validate 验证配置合法性。
func (c *VisionConfig) Validate() error {
	if c.CameraFPS < 1 || c.CameraFPS > 10 {
		return &ConfigError{Field: "camera_fps", Message: "must be in range [1, 10]"}
	}
	if c.FollowGapMs < 100 || c.FollowGapMs > 500 {
		return &ConfigError{Field: "follow_gap_ms", Message: "must be in range [100, 500]"}
	}
	if c.DeadZonePx < 8 || c.DeadZonePx > 20 {
		return &ConfigError{Field: "dead_zone_px", Message: "must be in range [8, 20]"}
	}
	if c.MinPulseUs >= c.MaxPulseUs {
		return &ConfigError{Field: "min/max_pulse_us", Message: "min must be less than max"}
	}
	return nil
}

// ConfigError 配置错误。
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return "vision config: " + e.Field + " " + e.Message
}

// =============================================================================
// FollowConfig 跟随配置（从 VisionConfig 派生）
// =============================================================================

// FollowConfig 几何转换配置。
type FollowConfig struct {
	FollowGapMs  int
	DeadZonePx   int
	HFovDeg      float64
	VFovDeg      float64
	MinPulseUs   int
	MaxPulseUs   int
	CenterPanUs  int
	CenterTiltUs int
	RangePanDeg  float64
	RangeTiltDeg float64
	InvertPan    bool
	InvertTilt   bool
}

// NewFollowConfig 从 VisionConfig 创建 FollowConfig。
func NewFollowConfig(cfg *VisionConfig, imageW, imageH int) *FollowConfig {
	aspectRatio := float64(imageW) / float64(imageH)
	vfovDeg := 2.0 * math.Atan(0.5*cfg.HFovDeg/aspectRatio)

	return &FollowConfig{
		FollowGapMs:  cfg.FollowGapMs,
		DeadZonePx:   cfg.DeadZonePx,
		HFovDeg:      cfg.HFovDeg,
		VFovDeg:      vfovDeg,
		MinPulseUs:   cfg.MinPulseUs,
		MaxPulseUs:   cfg.MaxPulseUs,
		CenterPanUs:  cfg.CenterPanUs,
		CenterTiltUs: cfg.CenterTiltUs,
		RangePanDeg:  cfg.RangePanDeg,
		RangeTiltDeg: cfg.RangeTiltDeg,
		InvertPan:    cfg.InvertPan,
		InvertTilt:   cfg.InvertTilt,
	}
}

// atan 计算反正切（弧度）。
func atan(x float64) float64 {
	return math.Atan(x)
}
