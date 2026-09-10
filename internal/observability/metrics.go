package observability

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/ykt/xiaozhi-server-go/internal/config"
)

// Metrics 可观测性指标收集器。
//
// 提供 Prometheus 格式的指标端点，暴露以下指标：
//   - ws_connections_active：活跃 WebSocket 连接数
//   - ws_messages_total：消息总数（按类型分组）
//   - ws_message_duration_seconds：消息处理耗时
//   - aisaas_requests_total：aisaas 请求总数（按接口分组）
//   - aisaas_request_duration_seconds：aisaas 请求耗时
//   - device_errors_total：设备错误总数
//   - xiaozhi_camera_frames_total：摄像头帧处理计数
//   - xiaozhi_servo_commands_total：舵机命令计数
//   - xiaozhi_face_identify_total：人脸识别计数
//   - xiaozhi_hello_auth_total：hello 认证计数
type Metrics struct {
	// WebSocket 连接数
	WSConnectionsActive prometheus.Gauge

	// WebSocket 消息计数
	WSMessagesTotal *prometheus.CounterVec

	// WebSocket 消息处理耗时
	WSMessageDuration *prometheus.HistogramVec

	// aisaas 请求计数
	AisaasRequestsTotal *prometheus.CounterVec

	// aisaas 请求耗时
	AisaasRequestDuration *prometheus.HistogramVec

	// 设备错误计数
	DeviceErrorsTotal *prometheus.CounterVec

	// === Vision 指标（视觉跟踪） ===
	// CameraFramesTotal 摄像头帧处理计数
	CameraFramesTotal *prometheus.CounterVec

	// CameraProcessingDur 摄像头帧处理耗时
	CameraProcessingDur *prometheus.HistogramVec

	// ServoCommandsTotal 舵机命令计数
	ServoCommandsTotal *prometheus.CounterVec

	// ServoThrottleDrop 舵机命令因节流丢弃计数
	ServoThrottleDrop *prometheus.CounterVec

	// FaceIdentifyTotal 人脸识别计数
	FaceIdentifyTotal *prometheus.CounterVec

	// HelloAuthTotal hello 认证计数
	HelloAuthTotal *prometheus.CounterVec
}

// NewMetrics 创建 Metrics 实例并注册到 Prometheus。
func NewMetrics(cfg config.MetricsConfig) *Metrics {
	m := &Metrics{
		WSConnectionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ws_connections_active",
			Help: "Number of active WebSocket connections",
		}),
		WSMessagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ws_messages_total",
			Help: "Total number of WebSocket messages",
		}, []string{"type", "direction"}),
		WSMessageDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "ws_message_duration_seconds",
			Help:    "WebSocket message processing duration",
			Buckets: prometheus.DefBuckets,
		}, []string{"type"}),
		AisaasRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aisaas_requests_total",
			Help: "Total number of aisaas API requests",
		}, []string{"method", "endpoint", "status"}),
		AisaasRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "aisaas_request_duration_seconds",
			Help:    "aisaas API request duration",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
		}, []string{"method", "endpoint"}),
		DeviceErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "device_errors_total",
			Help: "Total number of device errors",
		}, []string{"device_id", "error_type"}),
		// Vision 指标
		CameraFramesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_camera_frames_total",
			Help: "Total number of camera frames processed",
		}, []string{"device_id", "status"}),
		CameraProcessingDur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "xiaozhi_camera_processing_duration_seconds",
			Help:    "Camera frame processing duration in seconds",
			Buckets: prometheus.DefBuckets,
		}, []string{"device_id"}),
		ServoCommandsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_servo_commands_total",
			Help: "Total number of servo commands sent",
		}, []string{"device_id", "mode", "throttled"}),
		ServoThrottleDrop: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_servo_throttle_drop_total",
			Help: "Total number of servo commands dropped due to throttle",
		}, []string{"device_id"}),
		FaceIdentifyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_face_identify_total",
			Help: "Total number of face identification results",
		}, []string{"device_id", "status"}),
		HelloAuthTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "xiaozhi_hello_auth_total",
			Help: "Total number of hello authentication results",
		}, []string{"status"}),
	}

	return m
}

// MustRegister 将所有 Metrics 注册到 Prometheus DefaultRegisterer。
//
// 必须在启动阶段调用，否则 /metrics 端点返回空。
func (m *Metrics) MustRegister() {
	prometheus.MustRegister(
		m.WSConnectionsActive,
		m.WSMessagesTotal,
		m.WSMessageDuration,
		m.AisaasRequestsTotal,
		m.AisaasRequestDuration,
		m.DeviceErrorsTotal,
		// Vision 指标
		m.CameraFramesTotal,
		m.CameraProcessingDur,
		m.ServoCommandsTotal,
		m.ServoThrottleDrop,
		m.FaceIdentifyTotal,
		m.HelloAuthTotal,
	)
}

// Handler 返回 Prometheus HTTP handler。
func (m *Metrics) Handler() http.Handler {
	return promhttp.Handler()
}

// RecordWSMessage 记录 WebSocket 消息。
func (m *Metrics) RecordWSMessage(msgType, direction string) {
	// TODO(T11): 实现
	// m.WSMessagesTotal.WithLabelValues(msgType, direction).Inc()
}

// RecordAisaasRequest 记录 aisaas 请求。
func (m *Metrics) RecordAisaasRequest(method, endpoint, status string, duration float64) {
	// TODO(T11): 实现
	// m.AisaasRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	// m.AisaasRequestDuration.WithLabelValues(method, endpoint).Observe(duration)
}

// RecordCameraFrame 记录摄像头帧处理。
func (m *Metrics) RecordCameraFrame(deviceID, status string, duration float64) {
	m.CameraFramesTotal.WithLabelValues(deviceID, status).Inc()
	if duration > 0 {
		m.CameraProcessingDur.WithLabelValues(deviceID).Observe(duration)
	}
}

// RecordServoCommand 记录舵机命令发送。
func (m *Metrics) RecordServoCommand(deviceID, mode string, throttled bool) {
	throttledStr := "false"
	if throttled {
		throttledStr = "true"
	}
	m.ServoCommandsTotal.WithLabelValues(deviceID, mode, throttledStr).Inc()
}

// RecordServoThrottleDrop 记录因节流丢弃的舵机命令。
func (m *Metrics) RecordServoThrottleDrop(deviceID string) {
	m.ServoThrottleDrop.WithLabelValues(deviceID).Inc()
}

// RecordFaceIdentify 记录人脸识别结果。
func (m *Metrics) RecordFaceIdentify(deviceID, status string) {
	m.FaceIdentifyTotal.WithLabelValues(deviceID, status).Inc()
}

// RecordHelloAuth 记录 hello 认证结果。
func (m *Metrics) RecordHelloAuth(status string) {
	m.HelloAuthTotal.WithLabelValues(status).Inc()
}

// =============================================================================
// OTel Metrics（V4-O 阶段）
// =============================================================================

// OTelMetricsConfig OTel metrics 初始化配置。
type OTelMetricsConfig struct {
	ExporterURL string        // "otel-collector:4317"
	ServiceName string        // 服务名
	Interval    time.Duration // 采集间隔，默认 15s
}

// InitOTelMetrics 初始化 OTel metrics（OTLP gRPC exporter）。
//
// 与 Prometheus metrics 并行运行，提供统一的 OTel 遥测管道。
// 返回 shutdown 函数用于优雅关闭。若 ExporterURL 为空，返回 noop shutdown。
func InitOTelMetrics(cfg OTelMetricsConfig) (func(context.Context) error, error) {
	if cfg.ExporterURL == "" {
		return func(context.Context) error { return nil }, nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Second
	}

	exporter, err := otlpmetricgrpc.New(context.Background(),
		otlpmetricgrpc.WithEndpoint(cfg.ExporterURL),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(cfg.Interval),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}

// =============================================================================
// A. HTTP 请求指标
// =============================================================================

var (
	OTelHTTPRequestsTotal, _ = otel.Meter("http").Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total HTTP requests"),
	)
	OTelHTTPRequestDuration, _ = otel.Meter("http").Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request latency"),
		metric.WithUnit("s"),
	)
)

// OTelHTTPAttrs 返回 HTTP 指标的标准属性。
func OTelHTTPAttrs(method, path string, status int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.Int("status", status),
	}
}

// =============================================================================
// B. AI 调用指标
// =============================================================================

var (
	OTelAICallsTotal, _ = otel.Meter("ai").Int64Counter(
		"ai_calls_total",
		metric.WithDescription("Total AI calls (LLM/TTS/ASR)"),
	)
	OTelAITokensTotal, _ = otel.Meter("ai").Int64Counter(
		"ai_tokens_total",
		metric.WithDescription("Total AI tokens consumed"),
	)
	OTelAILatencySeconds, _ = otel.Meter("ai").Float64Histogram(
		"ai_latency_seconds",
		metric.WithDescription("AI call latency"),
		metric.WithUnit("s"),
	)
)

// OTelAIAttrs 返回 AI 调用指标的标准属性。
func OTelAIAttrs(model, provider, callType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("model", model),
		attribute.String("provider", provider),
		attribute.String("type", callType),
	}
}

// =============================================================================
// C. WebSocket 指标
// =============================================================================

var (
	OTelWSConnectionsActive, _ = otel.Meter("ws").Int64UpDownCounter(
		"ws_connections_active",
		metric.WithDescription("Active WebSocket connections"),
	)
	OTelWSMessagesTotal, _ = otel.Meter("ws").Int64Counter(
		"ws_messages_total",
		metric.WithDescription("Total WebSocket messages"),
	)
	OTelWSAudioFramesTotal, _ = otel.Meter("ws").Int64Counter(
		"ws_audio_frames_total",
		metric.WithDescription("Total audio frames processed"),
	)
)

// OTelWSAttrs 返回 WebSocket 指标的标准属性。
func OTelWSAttrs(msgType, direction string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("type", msgType),
		attribute.String("direction", direction),
	}
}

// =============================================================================
// D. Database 数据库指标
// =============================================================================

var (
	OTelDBQueriesTotal, _ = otel.Meter("db").Int64Counter(
		"db_queries_total",
		metric.WithDescription("Total DB queries"),
	)
	OTelDBConnectionsActive, _ = otel.Meter("db").Int64UpDownCounter(
		"db_connections_active",
		metric.WithDescription("Active DB connections"),
	)
	OTelDBQueryDuration, _ = otel.Meter("db").Float64Histogram(
		"db_query_duration_seconds",
		metric.WithDescription("DB query duration"),
		metric.WithUnit("s"),
	)
)

// OTelDBAttrs 返回数据库指标的标准属性。
func OTelDBAttrs(operation, table string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("operation", operation),
		attribute.String("table", table),
	}
}

// =============================================================================
// E. Device 设备指标
// =============================================================================

var (
	OTelDeviceErrorsTotal, _ = otel.Meter("device").Int64Counter(
		"device_errors_total",
		metric.WithDescription("Total device errors"),
	)
)

// OTelDeviceAttrs 返回设备指标的标准属性。
func OTelDeviceAttrs(deviceID, errorType string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("device_id", deviceID),
		attribute.String("error_type", errorType),
	}
}