package transport

// MessageType 定义设备通信消息类型。
//
// 与 xiaozhi-esp32-server-java V1 协议对齐：
//   - hello：握手消息（设备注册 + 能力协商）
//   - listen：监听状态变更（start/stop/detect）
//   - abort：中止当前对话
//   - iot：IoT 设备控制指令
type MessageType string

// 设备 → 服务端消息类型
const (
	MsgTypeHello  MessageType = "hello"  // 握手消息
	MsgTypeListen MessageType = "listen" // 监听状态变更
	MsgTypeAbort  MessageType = "abort"  // 中止对话
	MsgTypeIot    MessageType = "iot"    // IoT 控制指令
)

// 服务端 → 设备消息类型
const (
	MsgTypeServerHello MessageType = "hello" // 握手响应
	MsgTypeSTT         MessageType = "stt"   // 语音识别结果
	MsgTypeTTS         MessageType = "tts"   // 语音合成（开始/停止/数据）
	MsgTypeLLM         MessageType = "llm"   // 大模型推理结果
	MsgTypeText        MessageType = "text"  // 文本消息
)

// MessageState 消息状态标识。
type MessageState string

// 消息状态常量
const (
	StateStart   MessageState = "start"   // 开始
	StateStop    MessageState = "stop"    // 停止
	StateDetect  MessageState = "detect"  // 唤醒词检测
	StateAbort   MessageState = "abort"   // 中止
	StateSuccess MessageState = "success" // 成功
	StateError   MessageState = "error"   // 错误
)

// HelloMessage 握手消息体。
// 设备连接后发送的第一个 JSON 消息。
type HelloMessage struct {
	Type       string `json:"type"`        // 固定 "hello"
	Version    int    `json:"version"`     // 协议版本（当前 2）
	MACAddress string `json:"mac_address"` // 设备 MAC 地址
	DeviceID   string `json:"device_id"`   // 设备唯一 ID
	AppVersion string `json:"app_version"` // 固件版本
	ChipModel  string `json:"chip_model"`  // 芯片型号
}

// HelloResponse 握手响应。
type HelloResponse struct {
	Type       string `json:"type"`        // 固定 "hello"
	Transport  string `json:"transport"`   // 传输协议: "websocket"
	SessionID  string `json:"session_id"`  // 会话 ID
	ServerTime string `json:"server_time"` // 服务端时间（RFC3339）
	Version    int    `json:"version"`     // 支持的协议版本
}

// ListenMessage 监听状态消息。
type ListenMessage struct {
	Type  string `json:"type"`  // 固定 "listen"
	State string `json:"state"` // start / stop / detect
	Mode  string `json:"mode,omitempty"` // auto / manual / always
}

// TTSMessage TTS 状态消息。
type TTSMessage struct {
	Type  string `json:"type"`  // 固定 "tts"
	State string `json:"state"` // start / stop
}

// STTMessage 语音识别结果消息。
type STTMessage struct {
	Type string `json:"type"` // 固定 "stt"
	Text string `json:"text"` // 识别文本
}

// LLMMessage LLM 推理结果消息。
type LLMMessage struct {
	Type    string `json:"type"`    // 固定 "llm"
	Content string `json:"content"` // 推理内容
	IsFinal bool   `json:"is_final"` // 是否最终结果
}

// TextMessage 文本消息。
type TextMessage struct {
	Type    string `json:"type"`    // 固定 "text"
	Content string `json:"content"` // 文本内容
}

// IoTMessage IoT 控制消息。
type IoTMessage struct {
	Type     string `json:"type"`     // 固定 "iot"
	DeviceID string `json:"device_id"` // 目标 IoT 设备 ID
	Command  string `json:"command"`  // 控制指令
	Params   any    `json:"params,omitempty"` // 指令参数
}

// AbortMessage 中止消息。
type AbortMessage struct {
	Type   string `json:"type"`   // 固定 "abort"
	Reason string `json:"reason"` // 中止原因
}

// CameraMessage ESP32 摄像头图片消息。
type CameraMessage struct {
	Type     string `json:"type"`      // 固定 "camera"
	DeviceID string `json:"device_id"` // 设备 ID
	ImageURL string `json:"image_url"` // 图片 URL 或 Base64
	Prompt   string `json:"prompt"`    // 分析提示词
	ResultID string `json:"result_id"` // 结果关联 ID
}

// CameraResultMessage 摄像头图片分析结果消息。
type CameraResultMessage struct {
	Type        string   `json:"type"`         // 固定 "camera_result"
	DeviceID    string   `json:"device_id"`    // 设备 ID
	ResultID    string   `json:"result_id"`    // 结果关联 ID
	Description string   `json:"description"`  // 图片描述
	Tags        []string `json:"tags"`         // 标签列表
	Confidence  float64  `json:"confidence"`   // 置信度
	Error       string   `json:"error,omitempty"` // 错误信息
}

// CameraVideoMsg ESP32 摄像头视频流消息（设备 → 服务端）。
// 设备以固定频率（默认 1fps）发送视频帧，服务端进行流式分析。
type CameraVideoMsg struct {
	Type      string `json:"type"`        // 固定 "camera_video"
	DeviceID  string `json:"device_id"`   // 设备 ID
	ResultID  string `json:"result_id"`   // 结果关联 ID
	FrameCh   chan *VideoFrameData `json:"-"` // 帧数据通道（非 JSON 字段）
	Prompt    string `json:"prompt"`       // 分析提示词
	Model     string `json:"model"`        // 模型 ID
	SampleRate int   `json:"sample_rate"`  // 采样率（帧/秒）
	MaxFrames int    `json:"max_frames"`   // 最大帧数限制
}

// VideoFrameData 视频帧数据
type VideoFrameData struct {
	Timestamp   int64  `json:"timestamp"`    // 时间戳（毫秒，int64 跨平台兼容）
	FrameNumber int    `json:"frameNumber"`  // 帧序号
	ImageData   []byte `json:"-"`             // 原始图像数据（非 JSON 字段）
	Width       int    `json:"width"`         // 图像宽度
	Height      int    `json:"height"`        // 图像高度
	Data        string `json:"data"`          // Base64 编码的 JPEG 数据
}

// CameraVideoResultMsg 摄像头视频流分析结果消息（服务端 → 设备）。
type CameraVideoResultMsg struct {
	Type        string   `json:"type"`           // 固定 "camera_video_result"
	DeviceID    string   `json:"device_id"`      // 设备 ID
	ResultID    string   `json:"result_id"`      // 结果关联 ID
	FrameNumber int      `json:"frame_number"`   // 对应帧序号
	Timestamp   int64    `json:"timestamp"`      // 时间戳（毫秒）
	Description string   `json:"description"`     // 帧描述
	Tags        []string `json:"tags"`           // 标签列表
	Confidence  float64  `json:"confidence"`     // 置信度
	LatencyMs   int      `json:"latency_ms"`     // 处理延迟
	Error       string   `json:"error,omitempty"` // 错误信息
}

// =============================================================================
// Vision-Servo Protocol v2 新增消息类型
// =============================================================================

// Method 常量（视觉跟踪相关）
//
// v1 不发送 face_profile（无识别/无 PII）；该消息类型为协议预留，详见 docs/protocol/vision-servo-v1.md §4.2.2。
const (
	MethodServo        = "servo"          // 舵机控制指令（服务端 → 设备）
	MethodFaceTrackAck = "face_track_ack" // 人脸跟踪 ACK（设备 → 服务端）
	MethodCamFps       = "cam_fps"        // 摄像头动态调速（服务端 → 设备）
	MethodCancel       = "cancel"         // 抢占/取消（服务端 → 设备）
	MethodFlush        = "flush"          // 强制重置跟踪状态（设备 → 服务端）
)

// Capability 设备能力（hello 消息中携带）。
type Capability struct {
	Camera CameraCap `json:"camera"`
	Servo  ServoCap  `json:"servo"`
	Audio  AudioCap  `json:"audio"`
}

// CameraCap 摄像头能力。
type CameraCap struct {
	MaxW        int `json:"max_width"`
	MaxH        int `json:"max_height"`
	MaxFPS      int `json:"max_fps"`
	JPEGQuality int `json:"jpeg_quality"`
}

// ServoCap 舵机能力。
type ServoCap struct {
	Channels    []string `json:"channels"`
	MinPulseUs  int      `json:"min_pulse_us"`
	MaxPulseUs  int      `json:"max_pulse_us"`
	CenterPan   int      `json:"center_pan"`
	CenterTilt  int      `json:"center_tilt"`
	RangePanDeg float64  `json:"range_pan_deg"`
	RangeTiltDeg float64 `json:"range_tilt_deg"`
	InvertPan   bool     `json:"invert_pan"`
	InvertTilt  bool     `json:"invert_tilt"`
}

// AudioCap 音频能力。
type AudioCap struct {
	Codec     string `json:"codec"`
	SampleRate int   `json:"sample_rate"`
}

// HelloRequest 设备 hello 请求参数（vision-servo v2）。
type HelloRequest struct {
	DeviceID   string      `json:"device_id"`
	FWVersion  string      `json:"fw_version"`
	PBVer      int         `json:"pb_ver"`
	Token      string      `json:"token"`
	Capability *Capability `json:"capability"`
}

// HelloAckMsg 服务端 hello 响应。
type HelloAckMsg struct {
	OK               bool        `json:"ok"`
	SessionToken     string      `json:"session_token,omitempty"`
	SessionExpiresAt string      `json:"session_expires_at,omitempty"`
	Negotiated       Negotiated  `json:"negotiated,omitempty"`
	Error            *HelloError `json:"error,omitempty"`
}

// Negotiated 协商后的视觉跟踪参数。
type Negotiated struct {
	CameraFPS   int `json:"camera_fps"`
	FollowGapMs int `json:"follow_gap_ms"`
	DeadZonePx  int `json:"dead_zone_px"`
}

// HelloError hello 错误信息。
type HelloError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ServoCommandJSON 下行舵机控制指令。
type ServoCommandJSON struct {
	Method string       `json:"method"`
	Params ServoParams  `json:"params"`
}

// ServoParams 舵机参数。
type ServoParams struct {
	Seq        uint64 `json:"seq"`
	PanUs      int    `json:"pan_us"`
	TiltUs     int    `json:"tilt_us"`
	DurationMs int    `json:"duration_ms"`
}

// FaceTrackAckJSON 设备人脸跟踪 ACK（设备 → 服务端）。
type FaceTrackAckJSON struct {
	Method string             `json:"method"`
	Params FaceTrackAckParams `json:"params"`
}

// FaceTrackAckParams 人脸跟踪 ACK 参数。
type FaceTrackAckParams struct {
	Seq         uint64 `json:"seq"`
	Status      string `json:"status"` // "ok" | "blocked" | "servo_error"
	ActualPanUs int    `json:"actual_pan_us"`
	ActualTiltUs int   `json:"actual_tilt_us"`
	TsMs        int64  `json:"ts_ms"`
}

// CamFpsJSON 下行摄像头动态调速指令。
//
// 设备收到后立即生效，将当前 camera_frame 上报频率调整为 target_fps（1-10）。
// 服务端会同时调整令牌桶速率，保持两端速率一致。
type CamFpsJSON struct {
	Method    string         `json:"method"`
	Params    CamFpsParams   `json:"params"`
}

// CamFpsParams cam_fps 参数。
type CamFpsParams struct {
	TargetFPS int `json:"target_fps"` // 1-10
	Reason    string `json:"reason,omitempty"` // "user_request" | "high_load" | "low_confidence" 等
}

// CancelJSON 下行抢占/取消指令。
//
// 通知设备立即中止当前执行中的某类操作（如 TTS 播放、servo 动作、in-flight 任务）。
// 设备侧根据 req 字段决定取消什么。
type CancelJSON struct {
	Method string        `json:"method"`
	Params CancelParams  `json:"params"`
}

// CancelParams cancel 参数。
type CancelParams struct {
	Req  string `json:"req"`            // 请求 ID 或动作类型标识
	TsMs int64  `json:"ts_ms,omitempty"` // 服务端时间戳（毫秒）
}

// FlushMessage 设备上行 flush 消息：强制服务端重置视觉跟踪状态。
//
// 设备在以下场景使用：
//   - 用户说"重新看"等显式指令
//   - 检测到跟踪错误（舵机卡死、人脸长期偏离）
//   - 长时间未检测到目标后恢复
type FlushMessage struct {
	Method string       `json:"method"`
	Reason string       `json:"reason,omitempty"` // "user_command" | "tracking_error" | "idle_resume" | "manual"
}