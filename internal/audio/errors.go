// Package audio 提供 Opus 编解码和 VAD 语音活动检测。
//
// 音频处理管线：
//
//	设备上传 Opus 帧 → OpusDecode → PCM 16-bit → VAD → ASR → LLM → TTS → PCM → OpusEncode → 设备下发
//
// 编解码参数：
//   - 采样率：16kHz
//   - 帧长：60ms（960 samples）
//   - 比特率：16kbps
//   - 声道：单声道
package audio

import "errors"

// 音频处理错误。
var (
	// ErrInvalidFrameSize PCM 帧大小无效（必须是 1920 bytes = 960 samples × 2 bytes）。
	ErrInvalidFrameSize = errors.New("audio: invalid PCM frame size, expected 1920 bytes (960 samples × 16-bit)")

	// ErrCodecNotReady 编解码器未初始化。
	ErrCodecNotReady = errors.New("audio: codec not initialized")

	// ErrVADNotReady VAD 检测器未初始化。
	ErrVADNotReady = errors.New("audio: VAD not initialized")

	// ErrOpusEncodeFailed Opus 编码失败。
	ErrOpusEncodeFailed = errors.New("audio: opus encode failed")

	// ErrOpusDecodeFailed Opus 解码失败。
	ErrOpusDecodeFailed = errors.New("audio: opus decode failed")

	// ErrAECNotReady AEC 处理器未初始化或已降级。
	ErrAECNotReady = errors.New("audio: AEC processor not ready (degraded or not initialized)")

	// ErrAECFailed AEC 回声消除处理失败。
	ErrAECFailed = errors.New("audio: AEC echo cancellation failed")

	// ErrNSFailed 噪声抑制处理失败。
	ErrNSFailed = errors.New("audio: noise suppression failed")

	// ErrAGCFailed 自动增益控制失败。
	ErrAGCFailed = errors.New("audio: automatic gain control failed")

	// ErrPipelineNotReady 音频管线未完全初始化。
	ErrPipelineNotReady = errors.New("audio: pipeline not fully initialized")
)