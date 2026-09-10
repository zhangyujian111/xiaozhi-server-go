package vision

import (
	"github.com/prometheus/client_golang/prometheus"
)

// VisionMetrics 视觉跟踪指标收集器。
type VisionMetrics struct {
	cameraFramesTotal   *prometheus.CounterVec
	cameraProcessingDur *prometheus.HistogramVec
	servoCommandsTotal  *prometheus.CounterVec
	servoThrottleDrop   *prometheus.CounterVec
	faceDetectTotal     *prometheus.CounterVec
}

// NewVisionMetrics 创建视觉指标收集器。
func NewVisionMetrics() *VisionMetrics {
	m := &VisionMetrics{
		cameraFramesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_camera_frames_total",
			Help: "Total number of camera frames processed",
		}, []string{"device_id", "status"}),
		cameraProcessingDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "xiaozhi_camera_processing_duration_seconds",
			Help:    "Camera frame processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"device_id"}),
		servoCommandsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_servo_commands_total",
			Help: "Total number of servo commands sent",
		}, []string{"device_id", "mode", "throttled"}),
		servoThrottleDrop: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_servo_throttle_drop_total",
			Help: "Total number of servo commands dropped due to throttle",
		}, []string{"device_id"}),
		faceDetectTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_face_detect_total",
			Help: "Total number of face detection results",
		}, []string{"device_id", "status"}),
	}
	return m
}

// MustRegister 注册所有指标到 Prometheus。
func (m *VisionMetrics) MustRegister() {
	prometheus.MustRegister(
		m.cameraFramesTotal,
		m.cameraProcessingDur,
		m.servoCommandsTotal,
		m.servoThrottleDrop,
		m.faceDetectTotal,
	)
}

// RecordCameraFrame 记录摄像头帧处理。
func (m *VisionMetrics) RecordCameraFrame(deviceID, status string, duration float64) {
	m.cameraFramesTotal.WithLabelValues(deviceID, status).Inc()
	if duration > 0 {
		m.cameraProcessingDur.WithLabelValues(deviceID).Observe(duration)
	}
}

// RecordServoCommand 记录舵机命令发送。
func (m *VisionMetrics) RecordServoCommand(deviceID, mode string, throttled bool) {
	throttledStr := "false"
	if throttled {
		throttledStr = "true"
	}
	m.servoCommandsTotal.WithLabelValues(deviceID, mode, throttledStr).Inc()
}

// RecordServoThrottleDrop 记录因节流丢弃的舵机命令。
func (m *VisionMetrics) RecordServoThrottleDrop(deviceID string) {
	m.servoThrottleDrop.WithLabelValues(deviceID).Inc()
}

// RecordFaceDetect 记录人脸检测结果（v1：仅检测，无识别）。
func (m *VisionMetrics) RecordFaceDetect(deviceID, status string) {
	m.faceDetectTotal.WithLabelValues(deviceID, status).Inc()
}

// =============================================================================
// NoopMetrics 空实现（测试时使用）
// =============================================================================

// NoopMetrics 空指标收集器。
type NoopMetrics struct{}

func (NoopMetrics) RecordCameraFrame(deviceID, status string, duration float64) {}
func (NoopMetrics) RecordServoCommand(deviceID, mode string, throttled bool)    {}
func (NoopMetrics) RecordServoThrottleDrop(deviceID string)                     {}
func (NoopMetrics) RecordFaceDetect(deviceID, status string)                    {}
