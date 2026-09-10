// Package server 提供 JSON-RPC 回调实现（设备握手、音频流、中止、MCP、IoT）。
//
// 每个回调以工厂函数形式暴露，返回 transport.Handler 所需的回调签名。
// 回调内部通过 aisaas.Client 调用远端 AI 服务。
//
// 回调注册关系：
//   - OnHello  → transport.Handler.OnHello
//   - OnListen  → transport.Handler.OnListen
//   - OnAbort   → transport.Handler.OnAbort
//   - OnMCP     → transport.Handler.OnMCP
//   - OnIot     → transport.Handler.OnIot
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/ykt/xiaozhi-server-go/internal/audio"
	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
	"github.com/ykt/xiaozhi-server-go/internal/iot"
	"github.com/ykt/xiaozhi-server-go/internal/transport"
)

// =============================================================================
// OnHello — 设备首次连接握手
// =============================================================================

// NewOnHello 创建设备握手回调。
//
// 设备连接后发送的第一个 JSON-RPC 消息，包含设备信息（MAC、芯片型号、固件版本）。
// 回调验证设备身份后返回 Welcome 响应（含 session_id、服务端时间）。
//
// 流程：
//  1. 记录设备信息（MAC、型号、固件版本）
//  2. 尝试拉取该设备的 Persona 列表（失败不阻塞握手）
//  3. 返回 HelloResponse（session_id = deviceID）
func NewOnHello(client *aisaas.Client, logger *slog.Logger) func(ctx context.Context, msg *transport.HelloMessage) (*transport.HelloResponse, error) {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, msg *transport.HelloMessage) (*transport.HelloResponse, error) {
		deviceID := msg.DeviceID
		if deviceID == "" {
			deviceID = msg.MACAddress
		}

		logger.InfoContext(ctx, "device hello",
			"device_id", deviceID,
			"mac", msg.MACAddress,
			"chip", msg.ChipModel,
			"firmware", msg.AppVersion,
			"protocol_version", msg.Version,
		)

		// 尝试拉取 Persona 列表（非阻塞，失败不阻断握手）
		if client != nil {
			personas, err := client.ListPersonas(ctx, deviceID, 1)
			if err != nil {
				logger.WarnContext(ctx, "list personas failed, continuing without persona",
					"device_id", deviceID,
					"error", err,
				)
			} else if personas != nil && len(personas.Items) > 0 {
				logger.InfoContext(ctx, "persona found for device",
					"device_id", deviceID,
					"persona_count", len(personas.Items),
					"persona_name", personas.Items[0].Name,
				)
			}
		}

		return &transport.HelloResponse{
			Type:       "hello",
			Transport:  "websocket",
			SessionID:  deviceID,
			ServerTime: time.Now().UTC().Format(time.RFC3339),
			Version:    2,
		}, nil
	}
}

// =============================================================================
// OnListen — 音频流处理（完整管线：AEC + NS + AGC + Opus + VAD）
// =============================================================================

// listenSession 管理一个活跃的音频处理会话。
type listenSession struct {
	sessionID string
	cancel    context.CancelFunc
}

// NewOnListen 创建音频流监听回调。
//
// 设备发送 listen 状态变更：start（开始采集音频）、stop（停止采集）、detect（唤醒词检测到）。
//
// 完整管线：
//
//	上行（麦克风）: Opus解码 → AEC → NS → VAD → AGC → Opus编码 → ASR
//	下行（TTS）:   TTS PCM → AEC参考 → Opus编码 → 设备下发
//	业务:          CreateSession → ASR → WriteMemory → Chat → WriteMemory → TTS → EndSession
//
// 音频处理在独立 goroutine 中运行，通过 context 取消控制生命周期。
func NewOnListen(client *aisaas.Client, audioPipeline *audio.FullPipeline, logger *slog.Logger) func(ctx context.Context, conn transport.IConn, msg *transport.ListenMessage) error {
	if logger == nil {
		logger = slog.Default()
	}

	var mu sync.Mutex
	sessions := make(map[string]*listenSession)

	return func(ctx context.Context, conn transport.IConn, msg *transport.ListenMessage) error {
		deviceID := conn.DeviceID()

		// 添加 trace span
		ctx, span := otel.Tracer("xiaozhi-server").Start(ctx, "OnListen "+msg.State+" "+deviceID)
		defer span.End()
		span.SetAttributes(attribute.String("device.id", deviceID))

		switch msg.State {
		case "start":
			// 1. 创建 session 预扣配额
			session, err := client.CreateSession(ctx, deviceID, "llm_tokens_in", 1000, nil)
			if err != nil {
				logger.ErrorContext(ctx, "create session failed", "device_id", deviceID, "error", err)
				return err
			}
			sessionID := session.SessionID

			logger.InfoContext(ctx, "listen started",
				"session_id", sessionID,
				"device_id", deviceID,
				"quota_remaining", session.QuotaRemaining,
			)

			// 2. 创建可取消的子 context 用于音频处理 goroutine
			listenCtx, cancel := context.WithCancel(ctx)

			// 3. 存储会话信息（用于 stop 时取消）
			mu.Lock()
			sessions[deviceID] = &listenSession{sessionID: sessionID, cancel: cancel}
			mu.Unlock()

			// 4. 启动音频处理管线 goroutine
			go processAudioPipeline(listenCtx, conn, client, audioPipeline, logger, deviceID, sessionID, func() {
				// 清理：goroutine 退出时移除会话记录 + end session
				mu.Lock()
				delete(sessions, deviceID)
				mu.Unlock()

				endCtx, endCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer endCancel()
				if _, err := client.EndSession(endCtx, sessionID, aisaas.QuotaUsage{}, "success"); err != nil {
					logger.WarnContext(ctx, "end session failed", "session_id", sessionID, "error", err)
				} else {
					logger.InfoContext(ctx, "listen session ended", "session_id", sessionID, "device_id", deviceID)
				}
			})

		case "stop":
			mu.Lock()
			s, ok := sessions[deviceID]
			if ok {
				delete(sessions, deviceID)
			}
			mu.Unlock()

			if ok {
				logger.InfoContext(ctx, "listen stopped by client",
					"session_id", s.sessionID,
					"device_id", deviceID,
				)
				s.cancel()
			} else {
				logger.WarnContext(ctx, "listen stop without active session", "device_id", deviceID)
			}

		case "detect":
			logger.InfoContext(ctx, "wake word detected", "device_id", deviceID)

		default:
			logger.WarnContext(ctx, "unknown listen state",
				"state", msg.State,
				"device_id", deviceID,
			)
		}

		return nil
	}
}

// processAudioPipeline 音频处理主循环。
//
// 运行在独立 goroutine 中，从 conn.RecvAudio() 持续读取音频帧，
// 通过 AEC 管线处理（回声消除 → 降噪 → VAD → Opus 编码），
// 累积后送入 ASR → Chat → TTS 管线，TTS 音频馈送 AEC 参考信号，
// 结果通过 conn.SendAudio() 返回设备。
// ctx 取消时退出。
func processAudioPipeline(
	ctx context.Context,
	conn transport.IConn,
	client *aisaas.Client,
	audioPipeline *audio.FullPipeline,
	logger *slog.Logger,
	deviceID string,
	sessionID string,
	onExit func(),
) {
	defer onExit()

	// 重置 VAD 状态，为新会话做准备
	if audioPipeline != nil {
		audioPipeline.Reset()
	}

	const (
		maxFramesPerTurn = 500  // 单轮最大帧数（防止内存溢出）
		minAudioBytes    = 8000 // 最小累积字节数（约 1s PCM 16kHz 16-bit mono）
	)

	audioBuffer := make([]byte, 0, minAudioBytes*2)
	frameCount := 0

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "audio pipeline cancelled",
				"session_id", sessionID,
				"frames_processed", frameCount,
			)
			return

		case frame, ok := <-conn.RecvAudio():
			if !ok {
				logger.InfoContext(ctx, "audio channel closed",
					"session_id", sessionID,
					"frames_processed", frameCount,
				)
				return
			}

			frameCount++
			if frameCount > maxFramesPerTurn {
				logger.WarnContext(ctx, "max frames per turn reached",
					"session_id", sessionID,
					"max_frames", maxFramesPerTurn,
				)
				return
			}

			// 解析音频帧二进制头
			audioFrame, _, err := transport.DecodeFrame(frame)
			if err != nil {
				logger.WarnContext(ctx, "decode audio frame failed",
					"session_id", sessionID,
					"error", err,
				)
				continue
			}

			// 跳过 VAD 检测帧（Silero 帧由设备端 VAD 产生，不参与 ASR）
			if audioFrame.Type == transport.FrameTypeSilero {
				continue
			}

			// ============================================================
			// AEC 音频管线：Opus解码 → AEC → NS → VAD → AGC → Opus编码
			// ============================================================
			var opusEncoded []byte
			var isSpeech bool

			if audioPipeline != nil {
				// 1. Opus 解码 → PCM
				pcm, err := audioPipeline.DecodeOpus(audioFrame.Data)
				if err != nil {
					logger.WarnContext(ctx, "opus decode failed",
						"session_id", sessionID,
						"frame_type", audioFrame.Type.String(),
						"frame_len", len(audioFrame.Data),
						"error", err,
					)
					continue
				}

				// 2. 通过 FullPipeline 处理（带回声消除）
				encoded, speech, err := audioPipeline.ProcessOutbound(pcm)
				if err != nil {
					logger.WarnContext(ctx, "audio pipeline process failed",
						"session_id", sessionID,
						"error", err,
					)
					continue
				}

				opusEncoded = encoded
				isSpeech = speech
			} else {
				// 降级：无管线，直接使用原始 Opus 数据
				opusEncoded = audioFrame.Data
				isSpeech = true
			}

			// 跳过静音帧
			if !isSpeech {
				continue
			}

			// 累积音频数据（使用处理后的 Opus 数据用于 ASR）
			audioBuffer = append(audioBuffer, opusEncoded...)

			// 缓冲区不足，继续累积
			if len(audioBuffer) < minAudioBytes {
				continue
			}

			// ============================================================
			// 音频管线：ASR → Chat → TTS → 设备下发
			// ============================================================

			asrCtx, asrCancel := context.WithTimeout(ctx, 10*time.Second)
			asrResp, err := client.ASR(asrCtx, audioBuffer, "opus", "")
			asrCancel()
			audioBuffer = audioBuffer[:0] // 重置缓冲区

			if err != nil {
				logger.WarnContext(ctx, "asr failed",
					"session_id", sessionID,
					"error", err,
				)
				continue
			}
			if asrResp.Text == "" {
				continue
			}

			logger.InfoContext(ctx, "asr result",
				"session_id", sessionID,
				"text", asrResp.Text,
				"duration", asrResp.Duration,
			)

			// 写记忆：user message
			writeMsgCtx, writeMsgCancel := context.WithTimeout(ctx, 5*time.Second)
			_, wmErr := client.WriteMessage(writeMsgCtx, deviceID, &aisaas.WriteMemoryMessageRequest{
				SessionID: sessionID,
				Role:      "user",
				Content:   asrResp.Text,
			})
			writeMsgCancel()
			if wmErr != nil {
				logger.WarnContext(ctx, "write memory user message failed",
					"session_id", sessionID,
					"error", wmErr,
				)
			}

			// LLM Chat
			chatCtx, chatCancel := context.WithTimeout(ctx, 30*time.Second)
			chatResp, err := client.Chat(chatCtx, &aisaas.ChatRequest{
				Model: "gpt-4",
				Messages: []aisaas.ChatMessage{
					{Role: "user", Content: asrResp.Text},
				},
			})
			chatCancel()

			if err != nil {
				logger.WarnContext(ctx, "chat failed",
					"session_id", sessionID,
					"error", err,
				)
				continue
			}

			llmText := ""
			if len(chatResp.Choices) > 0 {
				llmText = chatResp.Choices[0].Message.Content
			}
			if llmText == "" {
				logger.WarnContext(ctx, "chat returned empty response",
					"session_id", sessionID,
				)
				continue
			}

			logger.InfoContext(ctx, "chat result",
				"session_id", sessionID,
				"text", llmText,
				"tokens", chatResp.Usage.TotalTokens,
			)

			// 写记忆：assistant message
			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			_, wmErr2 := client.WriteMessage(writeCtx, deviceID, &aisaas.WriteMemoryMessageRequest{
				SessionID: sessionID,
				Role:      "assistant",
				Content:   llmText,
			})
			writeCancel()
			if wmErr2 != nil {
				logger.WarnContext(ctx, "write memory assistant message failed",
					"session_id", sessionID,
					"error", wmErr2,
				)
			}

			// TTS
			ttsCtx, ttsCancel := context.WithTimeout(ctx, 15*time.Second)
			ttsAudio, err := client.TTS(ttsCtx, llmText, "default", "pcm")
			ttsCancel()

			if err != nil {
				logger.WarnContext(ctx, "tts failed",
					"session_id", sessionID,
					"error", err,
				)
				continue
			}
			if len(ttsAudio) == 0 {
				logger.WarnContext(ctx, "tts returned empty audio",
					"session_id", sessionID,
				)
				continue
			}

			// 馈送 TTS 下行音频到 AEC 参考信号（为后续麦克风帧提供回声参考）
			if audioPipeline != nil && audioPipeline.IsAECEnabled() {
				if err := audioPipeline.ProcessInbound(ttsAudio); err != nil {
					logger.WarnContext(ctx, "aec feed failed",
						"session_id", sessionID,
						"error", err,
					)
				}
			}

			// 编码为音频帧并发送到设备
			// 使用 FullPipeline 的 Opus 编码器（或直传 PCM 作为 fallback）
			var ttsFrameData []byte
			if audioPipeline != nil {
				encoded, encErr := audioPipeline.EncodeOpus(ttsAudio)
				if encErr != nil {
					logger.WarnContext(ctx, "tts opus encode failed, using pcm fallback",
						"session_id", sessionID,
						"error", encErr,
					)
					ttsFrameData = ttsAudio // fallback to raw PCM
				} else {
					ttsFrameData = encoded
				}
			} else {
				ttsFrameData = ttsAudio
			}

			ttsFrame := &transport.AudioFrame{
				Type: transport.FrameTypePCM,
				Data: ttsFrameData,
			}
			encodedFrame, err := transport.EncodeFrame(ttsFrame)
			if err != nil {
				logger.WarnContext(ctx, "encode tts frame failed",
					"session_id", sessionID,
					"error", err,
				)
				continue
			}

			if err := conn.SendAudio(encodedFrame); err != nil {
				logger.WarnContext(ctx, "send audio frame failed",
					"session_id", sessionID,
					"error", err,
				)
				continue
			}

			logger.InfoContext(ctx, "tts sent",
				"session_id", sessionID,
				"audio_bytes", len(ttsAudio),
				"frame_bytes", len(encodedFrame),
			)
		}
	}
}

// =============================================================================
// OnAbort — 中止当前对话
// =============================================================================

// NewOnAbort 创建中止对话回调。
//
// 设备发送 abort 取消当前语音交互（如用户打断、超时、错误）。
// 回调清理当前对话状态，释放资源。
//
// 注：当前 abort 通过 OnListen stop 路径处理（取消 context 传播到音频处理管线）。
// abort 回调仅记录事件，实际管线停止由 OnListen stop 触发。
func NewOnAbort(logger *slog.Logger) func(ctx context.Context, msg *transport.AbortMessage) error {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, msg *transport.AbortMessage) error {
		logger.InfoContext(ctx, "conversation aborted",
			"reason", msg.Reason,
		)
		return nil
	}
}

// =============================================================================
// OnMCP — MCP 工具调用（完整实现）
// =============================================================================

// NewOnMCP 创建 MCP 工具调用回调。
//
// 设备请求调用 MCP 工具（如天气查询、日程管理、智能家居控制）。
// 回调转发到 aisaas MCP 服务，返回工具执行结果。
//
// 流程：
//  1. 解析 MCPMessage（tool_name, arguments, session_id）
//  2. 调 aisaas.CallMCP() 转发到远端 MCP 服务
//  3. 返回 MCPResultMessage 给设备
func NewOnMCP(client *aisaas.Client, logger *slog.Logger) func(ctx context.Context, conn transport.IConn, msg *transport.MCPMessage) (*transport.MCPResultMessage, error) {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, conn transport.IConn, msg *transport.MCPMessage) (*transport.MCPResultMessage, error) {
		logger.InfoContext(ctx, "mcp tool call",
			"tool", msg.ToolName,
			"session_id", msg.SessionID,
			"device_id", conn.DeviceID(),
		)

		// 解析 arguments JSON 字符串为 map
		var args map[string]any
		if msg.Arguments != "" {
			if err := json.Unmarshal([]byte(msg.Arguments), &args); err != nil {
				logger.WarnContext(ctx, "mcp arguments parse failed",
					"tool", msg.ToolName,
					"arguments", msg.Arguments,
					"error", err,
				)
				// 参数解析失败不阻断调用，传空 map
				args = make(map[string]any)
			}
		}
		if args == nil {
			args = make(map[string]any)
		}

		// 调用 aisaas MCP 服务
		mcpResp, err := client.CallMCP(ctx, conn.DeviceID(), &aisaas.CallMCPRequest{
			Tool: msg.ToolName,
			Args: args,
		})
		if err != nil {
			logger.ErrorContext(ctx, "call mcp failed",
				"tool", msg.ToolName,
				"device_id", conn.DeviceID(),
				"error", err,
			)
			return &transport.MCPResultMessage{
				Type:      "mcp_result",
				ToolName:  msg.ToolName,
				Success:   false,
				Error:     err.Error(),
				SessionID: msg.SessionID,
			}, nil // 不返回 error，而是将错误封装在 result 中返回给设备
		}

		logger.InfoContext(ctx, "mcp call succeeded",
			"tool", msg.ToolName,
			"device_id", conn.DeviceID(),
			"result_len", len(mcpResp.Result),
		)

		return &transport.MCPResultMessage{
			Type:      "mcp_result",
			ToolName:  msg.ToolName,
			Success:   mcpResp.Error == "" && mcpResp.Result != "",
			Result:    mcpResp.Result,
			Error:     mcpResp.Error,
			SessionID: msg.SessionID,
		}, nil
	}
}

// =============================================================================
// OnIoT — IoT 设备控制
// =============================================================================

// NewOnIoT 创建 IoT 设备控制回调。
//
// 设备发送 IoT 控制指令（如控制智能灯、插座、空调等）。
// 回调通过 iot.Manager.Execute() 真实下发指令到 IoT 设备。
//
// P2 阶段：iot.Manager 内部使用 mock 50ms 延迟模拟指令执行。
// P3 阶段：升级到 MQTT transport（broker URL 通过 config 配置）。
func NewOnIoT(iotMgr iot.Manager, logger *slog.Logger) func(ctx context.Context, conn transport.IConn, msg *transport.IoTMessage) error {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, conn transport.IConn, msg *transport.IoTMessage) error {
		logger.InfoContext(ctx, "iot command received",
			"device_id", msg.DeviceID,
			"command", msg.Command,
			"params", msg.Params,
		)

		// 将 msg.Params (any) 转换为 map[string]any
		var params map[string]any
		switch v := msg.Params.(type) {
		case map[string]any:
			params = v
		case nil:
			params = make(map[string]any)
		default:
			// 尝试 JSON 序列化/反序列化转换
			params = make(map[string]any)
			params["value"] = v
		}

		result, err := iotMgr.Execute(ctx, msg.DeviceID, iot.Command{
			Action:     msg.Command,
			Parameters: params,
			Timeout:    5 * time.Second,
		})
		if err != nil {
			logger.ErrorContext(ctx, "iot execute failed",
				"device_id", msg.DeviceID,
				"command", msg.Command,
				"error", err,
			)
			return err
		}

		logger.InfoContext(ctx, "iot executed",
			"device_id", msg.DeviceID,
			"command", msg.Command,
			"success", result.Success,
			"latency_ms", result.LatencyMs,
		)
		return nil
	}
}

// =============================================================================
// OnCamera — ESP32 摄像头图片分析
// =============================================================================

// NewOnCamera 创建摄像头图片分析回调。
//
// 设备发送摄像头图片（Base64 或 URL），回调通过 aisaas.AnalyzeVision 进行理解分析，
// 返回图片描述、标签等信息给设备。
//
// 流程：
//  1. 接收 CameraMessage（包含 ImageURL 和 Prompt）
//  2. 调用 aisaas.AnalyzeVision 获取分析结果
//  3. 返回 CameraResultMessage 给设备
func NewOnCamera(visionClient *aisaas.Client, logger *slog.Logger) func(ctx context.Context, conn transport.IConn, msg *transport.CameraMessage) error {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, conn transport.IConn, msg *transport.CameraMessage) error {
		logger.InfoContext(ctx, "camera message received",
			"device_id", msg.DeviceID,
			"result_id", msg.ResultID,
			"prompt", msg.Prompt,
			"image_url_len", len(msg.ImageURL),
		)

		// 调用 aisaas 视觉理解服务
		resp, err := visionClient.AnalyzeVision(ctx, aisaas.VisionRequest{
			ImageURL:  msg.ImageURL,
			Prompt:    msg.Prompt,
			Model:     "qwen-vl-max",
			MaxTokens: 1024,
		})
		if err != nil {
			logger.ErrorContext(ctx, "vision analyze failed",
				"device_id", msg.DeviceID,
				"result_id", msg.ResultID,
				"error", err,
			)
			// 返回错误结果给设备
			resultMsg := &transport.CameraResultMessage{
				Type:        "camera_result",
				DeviceID:    msg.DeviceID,
				ResultID:    msg.ResultID,
				Description: "",
				Tags:        nil,
				Confidence:  0,
				Error:       err.Error(),
			}
			resultBytes, _ := json.Marshal(resultMsg)
			if sendErr := conn.SendCmd(resultBytes); sendErr != nil {
				logger.WarnContext(ctx, "send camera error result failed",
					"device_id", msg.DeviceID,
					"error", sendErr,
				)
			}
			return err
		}

		// 返回分析结果给设备
		resultMsg := &transport.CameraResultMessage{
			Type:        "camera_result",
			DeviceID:    msg.DeviceID,
			ResultID:    msg.ResultID,
			Description: resp.Description,
			Tags:        resp.Tags,
			Confidence:  resp.Confidence,
		}
		resultBytes, _ := json.Marshal(resultMsg)
		if err := conn.SendCmd(resultBytes); err != nil {
			logger.WarnContext(ctx, "send camera result failed",
				"device_id", msg.DeviceID,
				"error", err,
			)
			return err
		}

		logger.InfoContext(ctx, "vision analyzed",
			"device_id", msg.DeviceID,
			"result_id", msg.ResultID,
			"tags", resp.Tags,
			"latency_ms", resp.LatencyMs,
			"confidence", resp.Confidence,
		)
		return nil
	}
}

// =============================================================================
// OnCameraVideo — ESP32 摄像头视频流分析
// =============================================================================

// NewOnCameraVideo 创建摄像头视频流分析回调。
//
// 设备发送视频帧流（默认 1fps），回调对每帧调用 aisaas.AnalyzeVision 进行分析，
// 实时返回每帧的分析结果给设备。
//
// 流程：
//  1. 接收 CameraVideoMsg（包含 FrameCh、Prompt、Model、SampleRate）
//  2. 从 FrameCh 接收视频帧
//  3. 对每帧调用 aisaas.AnalyzeVision 获取分析结果
//  4. 返回 CameraVideoResultMsg 给设备
func NewOnCameraVideo(visionClient *aisaas.Client, logger *slog.Logger) func(ctx context.Context, conn transport.IConn, msg *transport.CameraVideoMsg) error {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, conn transport.IConn, msg *transport.CameraVideoMsg) error {
		logger.InfoContext(ctx, "camera video stream started",
			"device_id", msg.DeviceID,
			"model", msg.Model,
			"sample_rate", msg.SampleRate,
			"prompt", msg.Prompt,
		)

		// 设置默认值
		model := msg.Model
		if model == "" {
			model = "qwen-vl-max"
		}

		// 持续处理视频帧直到 context 取消或通道关闭
		for {
			select {
			case <-ctx.Done():
				logger.InfoContext(ctx, "camera video stream cancelled",
					"device_id", msg.DeviceID,
				)
				return nil

			case frame, ok := <-msg.FrameCh:
				if !ok {
					logger.InfoContext(ctx, "camera video stream channel closed",
						"device_id", msg.DeviceID,
					)
					return nil
				}

				// 构建 Base64 图片数据 URL
				imageURL := frame.Data
				if !strings.HasPrefix(imageURL, "data:") {
					imageURL = "data:image/jpeg;base64," + frame.Data
				}

				// 调用 aisaas 单帧分析
				resp, err := visionClient.AnalyzeVision(ctx, aisaas.VisionRequest{
					ImageURL:  imageURL,
					Prompt:    msg.Prompt,
					Model:     model,
					MaxTokens: 1024,
				})
				if err != nil {
					logger.WarnContext(ctx, "frame analyze failed",
						"device_id", msg.DeviceID,
						"frame_number", frame.FrameNumber,
						"error", err,
					)
					// 发送错误结果给设备（不中断流）
					resultMsg := &transport.CameraVideoResultMsg{
						Type:        "camera_video_result",
						DeviceID:    msg.DeviceID,
						FrameNumber: frame.FrameNumber,
						Error:       err.Error(),
					}
					resultBytes, _ := json.Marshal(resultMsg)
					if sendErr := conn.SendCmd(resultBytes); sendErr != nil {
						logger.WarnContext(ctx, "send frame error result failed",
							"device_id", msg.DeviceID,
							"error", sendErr,
						)
					}
					continue
				}

				// 发送分析结果到设备
				resultMsg := &transport.CameraVideoResultMsg{
					Type:        "camera_video_result",
					DeviceID:    msg.DeviceID,
					FrameNumber: frame.FrameNumber,
					Description: resp.Description,
					Tags:        resp.Tags,
					LatencyMs:   resp.LatencyMs,
				}
				resultBytes, _ := json.Marshal(resultMsg)
				if err := conn.SendCmd(resultBytes); err != nil {
					logger.WarnContext(ctx, "send video result failed",
						"device_id", msg.DeviceID,
						"frame_number", frame.FrameNumber,
						"error", err,
					)
					return err
				}

				logger.DebugContext(ctx, "frame analyzed",
					"device_id", msg.DeviceID,
					"frame_number", frame.FrameNumber,
					"latency_ms", resp.LatencyMs,
				)
			}
		}
	}
}

// =============================================================================
// OnServoAck — 设备回执舵机执行结果（vision-servo v2）
// =============================================================================

// NewOnServoAck 创建 servo ACK 回调。
//
// 设备执行完舵机控制指令后回执执行结果（成功/失败），回调仅记录日志。
//
// 当前 MVP：仅日志记录，未来可扩展为 metric 接入或重试逻辑。
func NewOnServoAck(logger *slog.Logger) func(ctx context.Context, params json.RawMessage) error {
	if logger == nil {
		logger = slog.Default()
	}

	return func(ctx context.Context, params json.RawMessage) error {
		var ack struct {
			Seq     uint64 `json:"seq"`
			Status  string `json:"status"`  // ok / error
			Message string `json:"message"` // 错误信息（如果有）
		}
		if err := json.Unmarshal(params, &ack); err != nil {
			logger.WarnContext(ctx, "servo ack parse failed", "error", err)
			return nil // 不阻断流程
		}

		if ack.Status == "ok" {
			logger.DebugContext(ctx, "servo command acknowledged",
				"seq", ack.Seq,
			)
		} else {
			logger.WarnContext(ctx, "servo command failed on device",
				"seq", ack.Seq,
				"message", ack.Message,
			)
		}
		return nil
	}
}