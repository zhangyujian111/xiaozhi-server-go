// Package transport 提供 JSON-RPC 消息分发 Handler。
//
// Handler 负责：
//   - WebSocket HTTP 升级（HTTP → WebSocket）
//   - 连接生命周期管理（心跳、超时、并发限制）
//   - JSON-RPC 2.0 方法路由与分发
//   - 设备握手（Hello）、音频流控制（Listen/Abort）等协议处理
//
// 方法路由表（methodRouter）映射 JSON-RPC method 到 handler 函数，
// 支持 15 种消息类型，覆盖设备握手、音频流、IoT 控制、MCP 工具调用等场景。
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
	"github.com/ykt/xiaozhi-server-go/internal/config"
	"github.com/ykt/xiaozhi-server-go/internal/vision"
)

// =============================================================================
// JSON-RPC 方法名常量（15 种消息类型）
// =============================================================================

const (
	MethodHello       = "hello"        // 设备握手
	MethodListen      = "listen"       // 开始/停止音频流
	MethodAbort       = "abort"        // 中止当前对话
	MethodTTS         = "tts"          // TTS 音频状态（server → device）
	MethodASR         = "stt"          // ASR 识别结果（server → device）
	MethodLLM         = "llm"          // LLM 推理结果（server → device）
	MethodText        = "text"         // 文本消息（server → device）
	MethodState       = "state"        // 设备状态更新（device → server）
	MethodMCP         = "mcp"          // MCP 工具调用
	MethodIot         = "iot"          // IoT 设备控制
	MethodMusic       = "music"        // 音乐播放控制
	MethodFile        = "file"         // 文件上传/下载
	MethodCamera      = "camera"       // 摄像头图片（device → server）
	MethodCameraVideo = "camera_video" // 摄像头视频流（device → server）
	MethodError       = "error"        // 错误通知（server → device）
	MethodGoodbye     = "goodbye"      // 断开连接
	MethodPing        = "ping"         // 心跳请求
	MethodPong        = "pong"         // 心跳响应
)

// =============================================================================
// 补充消息类型（T7 message.go 中未定义的部分）
// =============================================================================

// StateMessage 设备状态更新消息。
// 设备定时或状态变更时发送，服务端无需响应（通知）。
type StateMessage struct {
	Type      string `json:"type"`                // 固定 "state"
	Battery   int    `json:"battery,omitempty"`   // 电量百分比（0-100）
	Charging  bool   `json:"charging,omitempty"`  // 是否充电中
	Volume    int    `json:"volume,omitempty"`    // 音量（0-100）
	WiFiRSSI  int    `json:"wifi_rssi,omitempty"` // WiFi 信号强度（dBm）
	Uptime    int64  `json:"uptime,omitempty"`    // 运行时长（秒）
	FreeHeap  int    `json:"free_heap,omitempty"` // 可用堆内存（字节）
}

// MCPMessage MCP 工具调用消息。
// 设备请求调用 MCP 工具，由 xiaozhi-server-go 转发到 aisaas。
type MCPMessage struct {
	Type      string `json:"type"`       // 固定 "mcp"
	ToolName  string `json:"tool_name"`  // 工具名称
	Arguments string `json:"arguments"`  // 工具参数（JSON 字符串）
	SessionID string `json:"session_id"` // 会话 ID
}

// MCPResultMessage MCP 工具调用结果。
type MCPResultMessage struct {
	Type      string `json:"type"`                 // 固定 "mcp_result"
	ToolName  string `json:"tool_name"`            // 工具名称
	Success   bool   `json:"success"`              // 是否成功
	Result    string `json:"result,omitempty"`     // 结果（JSON 字符串）
	Error     string `json:"error,omitempty"`      // 错误信息
	SessionID string `json:"session_id"`           // 会话 ID
}

// FileMessage 文件操作消息。
// 支持文件上传（device → server）和下载（server → device）。
type FileMessage struct {
	Type     string `json:"type"`                // 固定 "file"
	Action   string `json:"action"`              // upload / download / delete / list
	FileName string `json:"file_name"`           // 文件名
	FileSize int64  `json:"file_size,omitempty"` // 文件大小（字节）
	Offset   int64  `json:"offset,omitempty"`    // 分片偏移量
	Chunk    []byte `json:"chunk,omitempty"`     // 文件分片数据（base64）
}

// FileResultMessage 文件操作结果。
type FileResultMessage struct {
	Type     string `json:"type"`                 // 固定 "file_result"
	Action   string `json:"action"`               // 对应操作
	FileName string `json:"file_name"`            // 文件名
	Success  bool   `json:"success"`              // 是否成功
	FileSize int64  `json:"file_size,omitempty"`  // 文件大小
	Error    string `json:"error,omitempty"`      // 错误信息
}

// MusicMessage 音乐播放控制消息（server → device）。
type MusicMessage struct {
	Type     string `json:"type"`                   // 固定 "music"
	Action   string `json:"action"`                 // play / pause / stop / next / prev
	TrackID  string `json:"track_id,omitempty"`     // 曲目 ID
	Title    string `json:"title,omitempty"`        // 曲目标题
	Artist   string `json:"artist,omitempty"`       // 艺术家
	Duration int    `json:"duration,omitempty"`     // 时长（秒）
	Volume   int    `json:"volume,omitempty"`       // 音量（0-100）
}

// GoodbyeMessage 断开连接消息。
// 设备主动关闭连接前发送，服务端收到后清理资源。
type GoodbyeMessage struct {
	Type   string `json:"type"`   // 固定 "goodbye"
	Reason string `json:"reason"` // 断开原因
}

// PingMessage 心跳请求/响应。
type PingMessage struct {
	Type      string `json:"type"`      // "ping" 或 "pong"
	Timestamp int64  `json:"timestamp"` // 发送时间戳（Unix 毫秒）
}

// =============================================================================
// Context Key — 将 IConn 传入业务回调
// =============================================================================

// connCtxKey 是 context 中存储 IConn 的 key。
// 用于在 JSON-RPC 方法处理器中获取当前连接实例。
type connCtxKey struct{}

// connFromCtx 从 context 中提取 IConn。未找到时返回 nil。
func connFromCtx(ctx context.Context) IConn {
	conn, _ := ctx.Value(connCtxKey{}).(IConn)
	return conn
}

// =============================================================================
// Handler 类型定义
// =============================================================================

// MethodHandler 是 JSON-RPC 方法处理函数的签名。
//
// 参数：
//   - h：Handler 实例
//   - ctx：连接上下文（可通过 connFromCtx 提取 IConn）
//   - params：方法参数（JSON 原始字节）
//
// 返回：
//   - interface{}：方法结果（将被 JSON 序列化为响应 result）
//   - error：处理错误（将转换为 JSON-RPC 错误响应）
type MethodHandler func(h *Handler, ctx context.Context, params json.RawMessage) (interface{}, error)

// Handler 管理 JSON-RPC 消息分发和设备连接。
//
// Handler 负责 WebSocket 连接升级、JSON-RPC 方法路由、连接生命周期管理。
// 业务回调（OnHello、OnListen 等）由上层（T10/T11）注册。
type Handler struct {
	transport Transport
	cfg       config.WebSocketConfig
	conns     map[string]IConn
	mu        sync.RWMutex

	// 业务回调——由上层注册。
	OnHello       func(ctx context.Context, msg *HelloMessage) (*HelloResponse, error)
	OnListen      func(ctx context.Context, conn IConn, msg *ListenMessage) error
	OnAbort       func(ctx context.Context, msg *AbortMessage) error
	OnIot         func(ctx context.Context, conn IConn, msg *IoTMessage) error
	OnState       func(ctx context.Context, msg *StateMessage) error
	OnMCP         func(ctx context.Context, conn IConn, msg *MCPMessage) (*MCPResultMessage, error)
	OnFile        func(ctx context.Context, msg *FileMessage) (*FileResultMessage, error)
	OnGoodbye     func(ctx context.Context, msg *GoodbyeMessage) error
	OnCamera      func(ctx context.Context, conn IConn, msg *CameraMessage) error
	OnCameraVideo func(ctx context.Context, conn IConn, msg *CameraVideoMsg) error

	// OnServoAck 设备回执舵机执行结果（vision-servo v2）
	OnServoAck func(ctx context.Context, params json.RawMessage) error

	// OnFlush 设备主动要求重置跟踪状态（vision-servo v2 §4.1）
	OnFlush func(ctx context.Context, conn IConn, msg *FlushMessage) error

	// Audio 音频处理器（Opus 编解码 + VAD）。
	Audio *AudioProcessor

	// vision 模式配置（vision-servo v2）。visionCfg.Enabled 为 true 时，
	// 每个 WebSocket 连接都会在 HandleWebSocket 内启动 per-conn 视觉流水线。
	visionAisaas *aisaas.Client
	visionCfg    *vision.VisionConfig
	visionLogger *slog.Logger
	visionImageW int
	visionImageH int

	// visionFlushHooks per-conn 设备 flush 钩子（vision-servo v2 §4.1）。
	// key 为 deviceID，value 为该设备 flush 时的处理函数（由 vision_pipeline 注册）。
	// 处理后自动注销。
	visionFlushHooks   map[string]func(msg *FlushMessage)
	visionFlushHooksMu sync.RWMutex

	logger zerolog.Logger
}

// NewHandler 创建 Handler 实例。
//
// 参数：
//   - transport：底层 Transport 实现（WebSocketTransport）
//   - cfg：WebSocket 配置（缓冲区大小、超时、连接限制等）
func NewHandler(transport Transport, cfg config.WebSocketConfig) *Handler {
	return &Handler{
		transport:        transport,
		cfg:              cfg,
		conns:            make(map[string]IConn),
		visionFlushHooks: make(map[string]func(msg *FlushMessage)),
		logger: log.With().
			Str("component", "transport.handler").
			Logger(),
	}
}

// SetVisionConfig 注入 vision 模式配置（vision-servo v2）。
//
// 调用方（main）在 vision.Enabled=true 时设置；之后每个 WebSocket 连接
// 都会在 HandleWebSocket 内启动 per-connection 视觉流水线。
//
// 参数：
//   - aisaas：aisaas 客户端（用于调用 IdentifyFace）
//   - cfg：vision 配置（可为 nil 表示禁用）
//   - logger：slog logger
//   - imageW/imageH：默认图像尺寸（设备 capability 未覆盖时使用）
func (h *Handler) SetVisionConfig(
	aisaasClient *aisaas.Client,
	cfg *vision.VisionConfig,
	logger *slog.Logger,
	imageW, imageH int,
) {
	h.visionAisaas = aisaasClient
	h.visionCfg = cfg
	h.visionLogger = logger
	h.visionImageW = imageW
	h.visionImageH = imageH
}

// ActiveConnections 返回当前活跃连接数。
func (h *Handler) ActiveConnections() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// =============================================================================
// WebSocket 连接处理
// =============================================================================

// HandleWebSocket 处理 HTTP → WebSocket 升级。
//
// 作为 Gin 路由的 handler 注册：
//
//	router.GET("/ws/:deviceId", handler.HandleWebSocket)
//
// 流程：
//  1. 解析 deviceId（URL 路径 /ws/{deviceId}）
//  2. 连接数检查（max_connections）
//  3. HTTP → WebSocket 升级
//  4. 创建 wsConn 实例
//  5. 启动读/写循环（goroutine）
//  6. 启动 JSON-RPC 分发循环（goroutine）
//  7. 返回（非阻塞）
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 1. 解析 deviceId
	deviceID := extractDeviceID(r.URL.Path)
	if deviceID == "" {
		http.Error(w, "missing deviceId in URL path (expected /ws/{deviceId})", http.StatusBadRequest)
		return
	}

	// 2. 连接数检查
	if h.cfg.MaxConnections > 0 {
		h.mu.RLock()
		connCount := len(h.conns)
		h.mu.RUnlock()
		if connCount >= h.cfg.MaxConnections {
			h.logger.Warn().
				Int("current", connCount).
				Int("max", h.cfg.MaxConnections).
				Str("device_id", deviceID).
				Msg("websocket connection limit reached")
			http.Error(w, "too many connections", http.StatusServiceUnavailable)
			return
		}
	}

	// 3. WebSocket 升级
	upgrader := websocket.Upgrader{
		ReadBufferSize:  h.cfg.ReadBufferSize,
		WriteBufferSize: h.cfg.WriteBufferSize,
		CheckOrigin: func(r *http.Request) bool {
			return true // IoT 设备无浏览器同源限制
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error().
			Err(err).
			Str("device_id", deviceID).
			Msg("websocket upgrade failed")
		return
	}

	// 4. 创建 wsConn
	ctx, cancel := context.WithCancel(context.Background())
	ws := &wsConn{
		deviceID:   deviceID,
		conn:       conn,
		ctx:        ctx,
		cancel:     cancel,
		cmdCh:      make(chan []byte, 64),
		audioCh:    make(chan []byte, 64),
		videoCh:    make(chan []byte, 64),
		writeCh:    make(chan []byte, 256),
		pingTicker: time.NewTicker(h.cfg.PingInterval),
	}

	// 5. 注册连接
	h.mu.Lock()
	h.conns[deviceID] = ws
	h.mu.Unlock()

	h.logger.Info().
		Str("device_id", deviceID).
		Str("remote_addr", r.RemoteAddr).
		Msg("device connected")

	// 6. 启动 goroutine（非阻塞，HandleWebSocket 立即返回）
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.conns, deviceID)
			h.mu.Unlock()
			ws.Close()
			h.logger.Info().
				Str("device_id", deviceID).
				Msg("device disconnected")
		}()

		// 启动写循环和 JSON-RPC 分发
		go h.writeLoop(ws)
		go h.serveJSONRPC(ctx, ws)

		// 启动 per-connection 视觉流水线（vision-servo v2，可选）
		h.startVisionPipeline(ctx, ws, deviceID)

		// 读循环阻塞直到连接关闭
		h.readLoop(ws)
	}()
}

// extractDeviceID 从 URL 路径提取设备 ID。
// 期望格式：/ws/{deviceId}
func extractDeviceID(path string) string {
	const prefix = "/ws/"
	if idx := strings.Index(path, prefix); idx >= 0 {
		return path[idx+len(prefix):]
	}
	return ""
}

// =============================================================================
// 读/写循环
// =============================================================================

// readLoop 从 WebSocket 读取消息，分发到 cmdCh（文本帧）或 audioCh/videoCh（二进制帧）。
//
// 阻塞直到连接关闭。连接关闭时取消 context，触发所有相关 goroutine 退出。
//
// 二进制帧分发规则（按首字节区分）：
//   - 0x00/0x01/0x02：音频帧 → audioCh
//   - 0x03：视频帧（JPEG）→ videoCh
//   - 其他：关闭连接
func (h *Handler) readLoop(ws *wsConn) {
	h.logger.Info().Str("device_id", ws.deviceID).Msg("readLoop entered")
	defer ws.cancel() // 取消 context，通知所有 goroutine 退出

	conn := ws.conn
	conn.SetReadLimit(h.cfg.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(h.cfg.PongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(h.cfg.PongWait))
		return nil
	})
	h.logger.Info().Str("device_id", ws.deviceID).Msg("readLoop waiting for ReadMessage")

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			h.logger.Info().
				Err(err).
				Str("device_id", ws.deviceID).
				Msg("readLoop ReadMessage returned error")
			if !websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived) {
				// 正常关闭，不记录错误
			} else {
				h.logger.Warn().
					Err(err).
					Str("device_id", ws.deviceID).
					Msg("websocket read error")
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			h.logger.Info().
				Str("device_id", ws.deviceID).
				Int("len", len(data)).
				Str("raw", string(data[:min(120, len(data))])).
				Msg("WS text frame received")
			// JSON-RPC 命令帧
			_ = data
		case websocket.BinaryMessage:
			// 二进制帧：根据首字节分发到 audioCh 或 videoCh
			if len(data) == 0 {
				h.logger.Warn().
					Str("device_id", ws.deviceID).
					Msg("empty binary frame, dropping")
				continue
			}
			frameType := data[0]
			h.logger.Info().
				Str("device_id", ws.deviceID).
				Int("len", len(data)).
				Uint8("frame_type", frameType).
				Str("hex_preview", fmt.Sprintf("%x", data[:min(32, len(data))])).
				Msg("WS binary frame received")
			switch {
			case frameType <= 0x02:
				// 音频帧 (0x00/0x01/0x02)
				select {
				case ws.audioCh <- data:
				case <-ws.ctx.Done():
					return
				default:
					h.logger.Warn().
						Str("device_id", ws.deviceID).
						Msg("audio channel full, dropping frame")
				}
			case frameType == 0x03:
				// 视频帧 (camera_frame)
				select {
				case ws.videoCh <- data:
				case <-ws.ctx.Done():
					return
				default:
					h.logger.Warn().
						Str("device_id", ws.deviceID).
						Msg("video channel full, dropping frame")
				}
			default:
				// 未知帧类型：仅记录 + 丢弃，不关闭连接（设备 hello/心跳可能用未登记 binary 类型）
				h.logger.Warn().
					Str("device_id", ws.deviceID).
					Uint8("frame_type", frameType).
					Int("len", len(data)).
					Str("hex_preview", fmt.Sprintf("%x", data[:min(32, len(data))])).
					Msg("unknown binary frame type, dropping (not closing)")
			}
		}
	}
}

// writeLoop 从 writeCh 读取数据并写入 WebSocket。
//
// 通过消息首字节判断帧类型：'{' 开头为文本帧（JSON-RPC），否则为二进制帧（音频）。
// 同时处理心跳 ping 发送。
func (h *Handler) writeLoop(ws *wsConn) {
	ticker := time.NewTicker(h.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case data, ok := <-ws.writeCh:
			if !ok {
				return
			}
			// 判断帧类型：JSON 以 '{' 开头，音频帧以二进制 header 开头
			msgType := websocket.BinaryMessage
			if len(data) > 0 && data[0] == '{' {
				msgType = websocket.TextMessage
			}

			_ = ws.conn.SetWriteDeadline(time.Now().Add(h.cfg.WriteWait))
			if err := ws.conn.WriteMessage(msgType, data); err != nil {
				h.logger.Warn().
					Err(err).
					Str("device_id", ws.deviceID).
					Msg("websocket write error")
				return
			}
		case <-ticker.C:
			_ = ws.conn.SetWriteDeadline(time.Now().Add(h.cfg.WriteWait))
			if err := ws.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				h.logger.Warn().
					Err(err).
					Str("device_id", ws.deviceID).
					Msg("websocket ping failed")
				return
			}
		case <-ws.ctx.Done():
			return
		}
	}
}

// =============================================================================
// JSON-RPC 分发
// =============================================================================

// ServeJSONRPC 启动 JSON-RPC 消息分发循环（非阻塞）。
//
// 在后台 goroutine 中运行，从 conn.RecvCmd() 读取 JSON-RPC 请求，
// 通过 methodRouter 路由到对应的 handler 函数。
//
// 当 conn 的 context 取消时自动退出。
func (h *Handler) ServeJSONRPC(ctx context.Context, conn IConn) error {
	// 已在 HandleWebSocket 中启动 goroutine，此处保留接口兼容
	return nil
}

// serveJSONRPC 在 goroutine 中运行 JSON-RPC 分发循环。
func (h *Handler) serveJSONRPC(ctx context.Context, conn IConn) {
	for {
		select {
		case data, ok := <-conn.RecvCmd():
			if !ok {
				return
			}
			h.logger.Info().
				Str("device_id", conn.DeviceID()).
				Int("len", len(data)).
				Str("preview", string(data[:min(120, len(data))])).
				Msg("json-rpc text frame received")
			h.dispatch(ctx, conn, data)
		case <-ctx.Done():
			return
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// dispatch 处理单条 JSON-RPC 消息。
//
// 同时兼容两种格式：
//   1. 严格 JSON-RPC 2.0：{"jsonrpc":"2.0","id":N,"method":"X","params":{...}}
//   2. 旧版 vision-servo v1 裸 type 消息：{"type":"hello","id":N,...}（无 jsonrpc envelope）
//
// 当 ParseRequest 失败但消息含顶层 "type" 字段时，回退到 legacy 调度。
func (h *Handler) dispatch(ctx context.Context, conn IConn, data []byte) {
	// 解析 JSON-RPC 请求
	req, err := ParseRequest(data)
	if err != nil {
		// legacy 格式 fallback：vision-servo v1 客户端发的是裸 {"type":"hello","id":N,...}
		if legacyType, id, params := parseLegacyEnvelope(data); legacyType != "" {
			h.dispatchLegacy(ctx, conn, legacyType, id, params)
			return
		}
		h.sendError(conn, nil, ErrCodeParse, "Parse error", err.Error())
		return
	}

	// 查找方法处理器
	handler, ok := methodRouter[req.Method]
	if !ok {
		h.sendError(conn, req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("Method %q not found", req.Method), nil)
		h.logger.Warn().
			Str("device_id", conn.DeviceID()).
			Str("method", req.Method).
			Msg("unknown JSON-RPC method")
		return
	}

	// 将 conn 注入 context，供业务回调提取
	ctx = context.WithValue(ctx, connCtxKey{}, conn)

	// 调用方法处理器
	result, err := handler(h, ctx, req.Params)
	if err != nil {
		h.sendError(conn, req.ID, ErrCodeInternal, err.Error(), nil)
		h.logger.Error().
			Err(err).
			Str("device_id", conn.DeviceID()).
			Str("method", req.Method).
			Msg("JSON-RPC method handler error")
		return
	}

	// 通知不需要响应
	if req.IsNotification() {
		return
	}

	// 构造成功响应
	resp, err := NewResponse(req.ID, result)
	if err != nil {
		h.sendError(conn, req.ID, ErrCodeInternal, "Failed to marshal response", err.Error())
		return
	}

	respData, err := resp.Marshal()
	if err != nil {
		return
	}
	_ = conn.SendCmd(respData)
}

// parseLegacyEnvelope 从裸 type 消息中提取 (type, id, params)。
//
// 返回值：
//   - type: 消息 type 字段（如 "hello"）；若无顶层 type 或 type 为空，返回 ""
//   - id: 顶层 id 字段（int64）；可为 nil（通知类）
//   - params: 去除 type/id 后的整个 JSON 对象作为 RawMessage
//
// 用途：vision-servo v1 客户端直接发 {"type":"hello","id":6,...} 而非
//      {"jsonrpc":"2.0","id":6,"method":"hello","params":{...}}，原 ParseRequest
//      会拒绝（"invalid version"）。这里给出 fallback。
func parseLegacyEnvelope(data []byte) (string, *int64, json.RawMessage) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return "", nil, nil
	}
	typeRaw, ok := probe["type"]
	if !ok {
		return "", nil, nil
	}
	var typeStr string
	if err := json.Unmarshal(typeRaw, &typeStr); err != nil || typeStr == "" {
		return "", nil, nil
	}
	var id *int64
	if idRaw, ok := probe["id"]; ok {
		var v int64
		if err := json.Unmarshal(idRaw, &v); err == nil {
			id = &v
		}
	}
	return typeStr, id, data
}

// dispatchLegacy 将 legacy type 消息路由到对应处理器。
//
// 当前支持：
//   - "hello"：调用 OnHello 回调，返回 hello_ack（裸 type，无 jsonrpc envelope）
//   - 其他 type：忽略（warn + 不响应，避免误关连接）
//
// 设备 ID 取自 conn.DeviceID()（URL 路径 /ws/:deviceId），与 OnHello 入参对齐。
func (h *Handler) dispatchLegacy(ctx context.Context, conn IConn, msgType string, id *int64, params json.RawMessage) {
	h.logger.Info().
		Str("device_id", conn.DeviceID()).
		Str("msg_type", msgType).
		Int("params_len", len(params)).
		Str("params_preview", string(params[:min(120, len(params))])).
		Msg("legacy message received")

switch msgType {
case "hello":
	if h.OnHello == nil {
		h.logger.Warn().Str("device_id", conn.DeviceID()).Msg("legacy hello: OnHello not registered")
		return
	}
	// 直接把 params 当作 HelloMessage
	var hello HelloMessage
	if err := json.Unmarshal(params, &hello); err != nil {
		h.logger.Warn().Err(err).Str("device_id", conn.DeviceID()).Msg("legacy hello: parse failed")
		return
	}
	resp, err := h.OnHello(ctx, &hello)
	if err != nil {
		h.logger.Error().Err(err).Str("device_id", conn.DeviceID()).Msg("legacy hello: OnHello error")
		return
	}
	// 构造 legacy hello_ack：保留顶层 type/id/transport/version/session_id/server_time
	// 不加 jsonrpc envelope（设备期望的就是裸 type 帧）
	ack := map[string]interface{}{
		"type":        resp.Type,
		"transport":   resp.Transport,
		"session_id":  resp.SessionID,
		"server_time": resp.ServerTime,
		"version":     resp.Version,
	}
	if id != nil {
		ack["id"] = *id
	}
	ackData, err := json.Marshal(ack)
	if err != nil {
		return
	}
	if err := conn.SendCmd(ackData); err != nil {
		h.logger.Warn().Err(err).Str("device_id", conn.DeviceID()).Msg("legacy hello: send ack failed")
	}
default:
	h.logger.Warn().
		Str("device_id", conn.DeviceID()).
		Str("msg_type", msgType).
		Msg("unknown legacy message type, ignoring")
}
}

// sendError 发送 JSON-RPC 错误响应。
func (h *Handler) sendError(conn IConn, id *int64, code int, message string, data interface{}) {
	resp, err := NewErrorResponse(id, code, message, data)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create error response")
		return
	}
	respData, err := resp.Marshal()
	if err != nil {
		return
	}
	_ = conn.SendCmd(respData)
}

// SendNotification 发送 JSON-RPC 通知到设备（无 id，不需要响应）。
//
// 用于 server → device 的单向消息：TTS 状态、ASR 结果、LLM 文本等。
func (h *Handler) SendNotification(conn IConn, method string, params interface{}) error {
	data, err := NewNotification(method, params)
	if err != nil {
		return fmt.Errorf("send notification %s: %w", method, err)
	}
	return conn.SendCmd(data)
}

// =============================================================================
// 方法路由表（15 种消息类型，9 个 device→server 方法）
// =============================================================================

// methodRouter 将 JSON-RPC method 映射到 Handler 方法。
//
// 使用 method expression 模式（(*Handler).methodName），
// 调用时传入 Handler 实例和参数。
var methodRouter = map[string]MethodHandler{
	// 设备 → 服务端方法
	MethodHello:       (*Handler).handleHello,
	MethodListen:      (*Handler).handleListen,
	MethodAbort:       (*Handler).handleAbort,
	MethodIot:         (*Handler).handleIot,
	MethodState:       (*Handler).handleState,
	MethodMCP:         (*Handler).handleMCP,
	MethodFile:        (*Handler).handleFile,
	MethodCamera:      (*Handler).handleCamera,
	MethodCameraVideo: (*Handler).handleCameraVideo,
	MethodGoodbye:     (*Handler).handleGoodbye,
	MethodFaceTrackAck: (*Handler).handleServoAck,
	MethodFlush:       (*Handler).handleFlush,

	// 心跳
	MethodPing: (*Handler).handlePing,
}

// =============================================================================
// 方法处理器实现
// =============================================================================

// handleHello 处理设备握手消息。
//
// 设备连接后发送的第一个消息，包含设备信息。
// 调用 OnHello 回调进行验证和会话创建。
func (h *Handler) handleHello(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnHello == nil {
		return nil, fmt.Errorf("hello handler not registered")
	}
	var msg HelloMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("hello: invalid params: %w", err)
	}
	if msg.MACAddress == "" && msg.DeviceID == "" {
		return nil, fmt.Errorf("hello: mac_address or device_id required")
	}
	return h.OnHello(ctx, &msg)
}

// handleListen 处理音频流控制消息。
//
// 设备发送 listen 状态变更：start / stop / detect。
// 调用 OnListen 回调处理音频流启动/停止。
func (h *Handler) handleListen(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnListen == nil {
		return nil, fmt.Errorf("listen handler not registered")
	}
	conn := connFromCtx(ctx)
	if conn == nil {
		return nil, fmt.Errorf("listen: conn not available in context")
	}
	var msg ListenMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("listen: invalid params: %w", err)
	}
	if err := h.OnListen(ctx, conn, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleAbort 处理中止对话消息。
//
// 设备发送 abort 取消当前语音交互。
// 调用 OnAbort 回调清理当前对话状态。
func (h *Handler) handleAbort(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnAbort == nil {
		return nil, fmt.Errorf("abort handler not registered")
	}
	var msg AbortMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("abort: invalid params: %w", err)
	}
	if err := h.OnAbort(ctx, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleIot 处理 IoT 设备控制消息。
//
// 设备发送 IoT 控制指令，如控制智能灯、插座等。
// 调用 OnIot 回调执行 IoT 设备控制。
func (h *Handler) handleIot(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnIot == nil {
		return nil, fmt.Errorf("iot handler not registered")
	}
	conn := connFromCtx(ctx)
	if conn == nil {
		return nil, fmt.Errorf("iot: conn not available in context")
	}
	var msg IoTMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("iot: invalid params: %w", err)
	}
	if err := h.OnIot(ctx, conn, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleState 处理设备状态更新消息（通知）。
//
// 设备定时发送状态信息（电量、信号强度等），无需响应。
// 调用 OnState 回调记录设备状态。
func (h *Handler) handleState(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnState == nil {
		return nil, nil // 未注册状态处理器时静默忽略
	}
	var msg StateMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("state: invalid params: %w", err)
	}
	if err := h.OnState(ctx, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleMCP 处理 MCP 工具调用消息。
//
// 设备请求调用 MCP 工具，返回工具执行结果。
// 调用 OnMCP 回调转发到 aisaas MCP 服务。
func (h *Handler) handleMCP(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnMCP == nil {
		return nil, fmt.Errorf("mcp handler not registered")
	}
	conn := connFromCtx(ctx)
	if conn == nil {
		return nil, fmt.Errorf("mcp: conn not available in context")
	}
	var msg MCPMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("mcp: invalid params: %w", err)
	}
	return h.OnMCP(ctx, conn, &msg)
}

// handleFile 处理文件操作消息。
//
// 支持文件上传/下载/删除/列表。
// 调用 OnFile 回调处理文件操作。
func (h *Handler) handleFile(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnFile == nil {
		return nil, fmt.Errorf("file handler not registered")
	}
	var msg FileMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("file: invalid params: %w", err)
	}
	return h.OnFile(ctx, &msg)
}

// handleGoodbye 处理设备断开连接消息。
//
// 设备主动关闭连接前发送，服务端清理资源。
// 调用 OnGoodbye 回调（可选，未注册时不报错）。
func (h *Handler) handleGoodbye(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnGoodbye == nil {
		// goodbye 是可选的，未注册时不报错
		return nil, nil
	}
	var msg GoodbyeMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("goodbye: invalid params: %w", err)
	}
	if err := h.OnGoodbye(ctx, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleCamera 处理摄像头图片消息。
//
// 设备发送摄像头图片（Base64 或 URL），调用 OnCamera 回调进行视觉理解分析。
func (h *Handler) handleCamera(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnCamera == nil {
		return nil, fmt.Errorf("camera handler not registered")
	}
	conn := connFromCtx(ctx)
	if conn == nil {
		return nil, fmt.Errorf("camera: conn not available in context")
	}
	var msg CameraMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("camera: invalid params: %w", err)
	}
	if err := h.OnCamera(ctx, conn, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handlePing 处理心跳请求。
//
// 返回 pong 响应，用于连接存活检测。
func (h *Handler) handlePing(ctx context.Context, params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"type":      "pong",
		"timestamp": time.Now().UnixMilli(),
	}, nil
}

// handleCameraVideo 处理摄像头视频流消息。
//
// 设备发送摄像头视频帧流，调用 OnCameraVideo 回调进行流式视觉理解分析。
func (h *Handler) handleCameraVideo(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnCameraVideo == nil {
		return nil, fmt.Errorf("camera_video handler not registered")
	}
	conn := connFromCtx(ctx)
	if conn == nil {
		return nil, fmt.Errorf("camera_video: conn not available in context")
	}
	var msg CameraVideoMsg
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("camera_video: invalid params: %w", err)
	}
	// 创建帧通道
	msg.FrameCh = make(chan *VideoFrameData, 64)
	if err := h.OnCameraVideo(ctx, conn, &msg); err != nil {
		return nil, err
	}
	return nil, nil
}

// handleServoAck 处理设备舵机执行结果回执（vision-servo v2）。
//
// 设备执行完舵机控制指令后回执结果，调用 OnServoAck 回调记录日志。
func (h *Handler) handleServoAck(ctx context.Context, params json.RawMessage) (interface{}, error) {
	if h.OnServoAck != nil {
		if err := h.OnServoAck(ctx, params); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// handleFlush 处理设备上行 flush 消息（vision-servo v2 §4.1）。
//
// 设备请求服务端重置视觉跟踪状态（清空 tracks、servo time、profile 记忆）。
// 默认会调用该设备的 vision_flush_hook（由 vision_pipeline 注册）。
// 同时调用用户注册的 OnFlush 回调（如果有），用于日志/埋点。
func (h *Handler) handleFlush(ctx context.Context, params json.RawMessage) (interface{}, error) {
	conn := connFromCtx(ctx)
	if conn == nil {
		return nil, fmt.Errorf("flush: conn not available in context")
	}
	var msg FlushMessage
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, fmt.Errorf("flush: invalid params: %w", err)
	}

	// 1. 触发 vision per-conn hook（默认重置 follower 状态）
	h.visionFlushHooksMu.RLock()
	hook, hasHook := h.visionFlushHooks[conn.DeviceID()]
	h.visionFlushHooksMu.RUnlock()
	if hasHook {
		hook(&msg)
	}

	// 2. 触发用户回调（用于日志/埋点，可选）
	if h.OnFlush != nil {
		if err := h.OnFlush(ctx, conn, &msg); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// registerVisionFlushHook 注册 per-conn flush 钩子（vision-servo v2）。
//
// 由 vision_pipeline 在 HandleWebSocket 内调用，绑定 deviceID → reset handler。
// 重复注册同一 deviceID 会覆盖。
func (h *Handler) registerVisionFlushHook(deviceID string, hook func(msg *FlushMessage)) {
	h.visionFlushHooksMu.Lock()
	defer h.visionFlushHooksMu.Unlock()
	h.visionFlushHooks[deviceID] = hook
}

// unregisterVisionFlushHook 注销 per-conn flush 钩子（连接关闭时调用）。
func (h *Handler) unregisterVisionFlushHook(deviceID string) {
	h.visionFlushHooksMu.Lock()
	defer h.visionFlushHooksMu.Unlock()
	delete(h.visionFlushHooks, deviceID)
}
