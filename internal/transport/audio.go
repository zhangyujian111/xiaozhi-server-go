// Package transport 提供音频流处理（Opus 编解码 + VAD 接口）。
//
// 音频处理管线：
//
//	设备上传 Opus 帧 → OpusDecode → PCM 16-bit → VAD → ASR → LLM → TTS → PCM → OpusEncode → 设备下发
//
// 二进制帧格式（与 xiaozhi-esp32-server-golang 对齐）：
//
//	[4 bytes: frame_length (big-endian)] [1 byte: frame_type] [N bytes: audio data]
//
// 帧类型：
//   - FrameTypeOpus (0x00)：Opus 编码音频数据
//   - FrameTypePCM  (0x01)：PCM 16-bit 未压缩音频（调试用）
//   - FrameTypeSilero (0x02)：VAD 检测结果
//
// Opus 编码参数：
//   - 采样率：16kHz
//   - 帧长：60ms（960 samples）
//   - 比特率：16kbps
//   - 声道：单声道
//
// 依赖：
//   - github.com/hraban/opus v2.0.0+（T10 集成）
//   - github.com/streamer45/silero-vad-go（T10 集成，VAD 检测）
package transport

import (
	"encoding/binary"
	"fmt"
)

// =============================================================================
// 音频帧类型
// =============================================================================

// AudioFrameType 音频帧类型标识。
type AudioFrameType byte

// 音频帧类型常量。
const (
	FrameTypeOpus   AudioFrameType = 0x00 // Opus 编码音频
	FrameTypePCM    AudioFrameType = 0x01 // PCM 16-bit 未压缩音频（调试用）
	FrameTypeSilero AudioFrameType = 0x02 // Silero VAD 检测结果
)

// AudioFrameHeaderSize 是二进制帧头的固定大小。
// 格式：[4 bytes frame_length][1 byte frame_type] = 5 bytes
const AudioFrameHeaderSize = 5

// AudioFrame 表示一个二进制音频帧。
//
// 用于设备与服务器之间的音频数据传输。
// 原始帧格式：[4 bytes len][1 byte type][N bytes data]
type AudioFrame struct {
	Type AudioFrameType // 帧类型
	Data []byte         // 音频数据
}

// EncodeFrame 将 AudioFrame 编码为二进制帧格式。
//
// 输出格式：
//
//	[4 bytes: total_length (big-endian)] [1 byte: frame_type] [N bytes: data]
//
// total_length 包含帧头自身（5 bytes）+ data 长度。
func EncodeFrame(frame *AudioFrame) ([]byte, error) {
	if frame == nil {
		return nil, fmt.Errorf("audio: nil frame")
	}
	totalLen := AudioFrameHeaderSize + len(frame.Data)
	buf := make([]byte, totalLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLen))
	buf[4] = byte(frame.Type)
	copy(buf[5:], frame.Data)
	return buf, nil
}

// DecodeFrame 从原始字节解码二进制帧头。
//
// 返回解码后的 AudioFrame 和剩余未处理字节（用于处理粘包场景）。
// 如果数据不足一个完整帧，返回错误。
func DecodeFrame(data []byte) (*AudioFrame, []byte, error) {
	if len(data) < AudioFrameHeaderSize {
		return nil, nil, fmt.Errorf("audio: frame too short (%d bytes, need at least %d)", len(data), AudioFrameHeaderSize)
	}
	totalLen := binary.BigEndian.Uint32(data[0:4])
	if len(data) < int(totalLen) {
		return nil, nil, fmt.Errorf("audio: incomplete frame (need %d bytes, have %d)", totalLen, len(data))
	}
	frame := &AudioFrame{
		Type: AudioFrameType(data[4]),
		Data: make([]byte, int(totalLen)-AudioFrameHeaderSize),
	}
	copy(frame.Data, data[5:totalLen])
	return frame, data[totalLen:], nil
}

// String 返回帧类型的可读描述。
func (t AudioFrameType) String() string {
	switch t {
	case FrameTypeOpus:
		return "opus"
	case FrameTypePCM:
		return "pcm"
	case FrameTypeSilero:
		return "silero(vad)"
	default:
		return fmt.Sprintf("unknown(0x%02x)", byte(t))
	}
}

// =============================================================================
// Opus 编解码接口
// =============================================================================

// OpusEncoder Opus 音频编码器接口。
//
// 将 PCM 16-bit 单声道音频编码为 Opus 格式。
// 具体实现使用 github.com/hraban/opus（T10 集成）。
type OpusEncoder interface {
	// Encode 将 PCM 帧编码为 Opus 帧。
	//
	// 参数：
	//   - pcm：PCM 16-bit 采样数组（16kHz, 单声道, 960 samples = 60ms）
	//
	// 返回：
	//   - []byte：Opus 编码数据
	//   - error：编码错误
	Encode(pcm []int16) ([]byte, error)

	// Reset 重置编码器状态（用于新对话开始时清空内部缓冲）。
	Reset() error

	// Close 释放编码器资源。
	Close() error
}

// OpusDecoder Opus 音频解码器接口。
//
// 将 Opus 格式解码为 PCM 16-bit 单声道音频。
// 具体实现使用 github.com/hraban/opus（T10 集成）。
type OpusDecoder interface {
	// Decode 将 Opus 帧解码为 PCM。
	//
	// 参数：
	//   - opus：Opus 编码数据
	//
	// 返回：
	//   - []int16：PCM 16-bit 采样数组（16kHz, 单声道）
	//   - error：解码错误
	Decode(opus []byte) ([]int16, error)

	// Reset 重置解码器状态。
	Reset() error

	// Close 释放解码器资源。
	Close() error
}

// =============================================================================
// VAD 接口
// =============================================================================

// VADDetector 语音活动检测器接口。
//
// 用于检测 PCM 音频帧中是否包含人声。
// 具体实现使用 github.com/streamer45/silero-vad-go（T10 集成）。
type VADDetector interface {
	// Detect 检测 PCM 帧中是否有语音活动。
	//
	// 参数：
	//   - pcm：PCM 16-bit 采样数组（16kHz, 单声道, 960 samples = 60ms）
	//
	// 返回：
	//   - bool：true 表示检测到人声，false 表示静音
	//   - error：检测错误
	Detect(pcm []int16) (bool, error)

	// Reset 重置 VAD 状态（用于新对话开始时清空上下文）。
	Reset() error

	// Close 释放 VAD 资源。
	Close() error
}

// =============================================================================
// AudioProcessor
// =============================================================================

// AudioProcessor 音频处理器，管理 Opus 编解码和 VAD 检测。
//
// 生命周期：
//  1. 创建 AudioProcessor（NewAudioProcessor）
//  2. 在 T10 阶段注入具体实现（SetEncoder / SetDecoder / SetVAD）
//  3. 连接建立时创建编解码器实例（NewEncoder / NewDecoder / NewVAD）
//  4. 连接关闭时释放资源（Close）
//
// 使用示例：
//
//	ap := NewAudioProcessor(DefaultAudioConfig())
//	ap.SetEncoder(myOpusEncoder)
//	ap.SetDecoder(myOpusDecoder)
//	ap.SetVAD(myVAD)
type AudioProcessor struct {
	encoder OpusEncoder
	decoder OpusDecoder
	vad     VADDetector

	// 配置
	sampleRate  int
	frameSizeMs int
	channels    int
	frameSize   int // 每帧采样数（sampleRate * frameSizeMs / 1000）
}

// AudioConfig 音频处理配置。
type AudioConfig struct {
	SampleRate  int // 采样率（Hz），默认 16000
	FrameSizeMs int // 帧长（ms），默认 60
	Channels    int // 声道数，默认 1
	Bitrate     int // Opus 编码比特率（bps），默认 16000
}

// DefaultAudioConfig 返回默认音频配置。
//
// 默认值：
//   - 采样率：16kHz
//   - 帧长：60ms（960 samples）
//   - 声道：单声道
//   - 比特率：16kbps
func DefaultAudioConfig() AudioConfig {
	return AudioConfig{
		SampleRate:  16000,
		FrameSizeMs: 60,
		Channels:    1,
		Bitrate:     16000,
	}
}

// NewAudioProcessor 创建音频处理器。
//
// T10 阶段注入具体编解码器和 VAD 实现。
// 当前（T9）仅设置配置和接口占位。
func NewAudioProcessor(cfg AudioConfig) *AudioProcessor {
	return &AudioProcessor{
		sampleRate:  cfg.SampleRate,
		frameSizeMs: cfg.FrameSizeMs,
		channels:    cfg.Channels,
		frameSize:   cfg.SampleRate * cfg.FrameSizeMs / 1000,
	}
}

// SetEncoder 设置 Opus 编码器。
func (ap *AudioProcessor) SetEncoder(enc OpusEncoder) {
	ap.encoder = enc
}

// SetDecoder 设置 Opus 解码器。
func (ap *AudioProcessor) SetDecoder(dec OpusDecoder) {
	ap.decoder = dec
}

// SetVAD 设置 VAD 检测器。
func (ap *AudioProcessor) SetVAD(vad VADDetector) {
	ap.vad = vad
}

// IsReady 检查编码器、解码器和 VAD 是否已初始化。
func (ap *AudioProcessor) IsReady() bool {
	return ap.encoder != nil && ap.decoder != nil && ap.vad != nil
}

// Encode 将 PCM 编码为 Opus。
//
// 前置条件：需先调用 SetEncoder 注入编码器实例。
func (ap *AudioProcessor) Encode(pcm []int16) ([]byte, error) {
	if ap.encoder == nil {
		return nil, fmt.Errorf("audio: encoder not initialized (call SetEncoder first)")
	}
	return ap.encoder.Encode(pcm)
}

// Decode 将 Opus 解码为 PCM。
//
// 前置条件：需先调用 SetDecoder 注入解码器实例。
func (ap *AudioProcessor) Decode(opus []byte) ([]int16, error) {
	if ap.decoder == nil {
		return nil, fmt.Errorf("audio: decoder not initialized (call SetDecoder first)")
	}
	return ap.decoder.Decode(opus)
}

// DetectVoice 检测 PCM 帧中是否有语音活动。
//
// 前置条件：需先调用 SetVAD 注入 VAD 检测器实例。
func (ap *AudioProcessor) DetectVoice(pcm []int16) (bool, error) {
	if ap.vad == nil {
		return false, fmt.Errorf("audio: VAD not initialized (call SetVAD first)")
	}
	return ap.vad.Detect(pcm)
}

// FrameSize 返回每帧采样数。
func (ap *AudioProcessor) FrameSize() int {
	return ap.frameSize
}

// SampleRate 返回采样率。
func (ap *AudioProcessor) SampleRate() int {
	return ap.sampleRate
}

// Close 释放音频处理器资源。
func (ap *AudioProcessor) Close() error {
	var errs []error
	if ap.encoder != nil {
		if err := ap.encoder.Close(); err != nil {
			errs = append(errs, fmt.Errorf("encoder close: %w", err))
		}
	}
	if ap.decoder != nil {
		if err := ap.decoder.Close(); err != nil {
			errs = append(errs, fmt.Errorf("decoder close: %w", err))
		}
	}
	if ap.vad != nil {
		if err := ap.vad.Close(); err != nil {
			errs = append(errs, fmt.Errorf("vad close: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("audio: close errors: %v", errs)
	}
	return nil
}