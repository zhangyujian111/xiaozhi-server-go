// Package audio 提供完整音频处理管线（AEC + Opus + VAD + NS + AGC）。
//
// 管线集成所有音频处理组件，提供统一的处理接口。
// 上层（handlers）通过 FullPipeline 完成所有音频处理，无需关心底层组件。
//
// 处理流程：
//
//	下行（TTS 播放）：
//	  TTS PCM → ProcessInbound → AEC 参考缓冲 → [发送到设备]
//
//	上行（麦克风输入）：
//	  设备 Opus → DecodeOpus → PCM → ProcessOutbound → AEC → NS → VAD → AGC → Opus → ASR
//
// 降级策略：
//
//	WebRTC APM 初始化失败 → FullPipeline 自动降级为无 AEC 模式
//	降级后仍然保留 Opus 编解码和 VAD 功能
package audio

import (
	"log/slog"
)

// FullPipeline 完整音频处理管线。
//
// 组合 AEC（回声消除）+ OpusCodec（编解码）+ EnergyVAD（语音检测）+ NoiseSuppressor（降噪）。
// 提供 ProcessInbound（下行参考）和 ProcessOutbound（上行处理）两个核心方法。
type FullPipeline struct {
	aec        *AECProcessor
	opusCodec  *OpusCodec
	vad        *EnergyVAD
	ns         *NoiseSuppressor
	sampleRate uint32
	frameSize  int // 每帧采样数（960 samples at 16kHz）
	logger     *slog.Logger

	// 降级标记
	aecEnabled bool
}

// NewFullPipeline 创建完整音频处理管线。
//
// 初始化顺序：AEC → Opus → VAD → NS
// AEC 初始化失败时自动降级为无 AEC 模式（保留 Opus + VAD）。
func NewFullPipeline() (*FullPipeline, error) {
	return NewFullPipelineWithLogger(nil)
}

// NewFullPipelineWithLogger 创建带日志器的完整音频处理管线。
//
// logger 为 nil 时使用 slog.Default()。
func NewFullPipelineWithLogger(logger *slog.Logger) (*FullPipeline, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. 初始化 AEC 处理器
	aec, aecErr := NewAECProcessor(16000, 1)
	aecEnabled := aecErr == nil

	if aecErr != nil {
		logger.Warn("aec init failed, degrading to no-aec mode",
			"error", aecErr,
			"effect", "echo cancellation disabled, opus+vad still available",
		)
	}

	// 2. 初始化 Opus 编解码器
	opus, err := NewOpusCodec()
	if err != nil {
		return nil, err
	}

	// 3. 初始化 NS（噪声抑制）
	ns := NewNoiseSuppressor()

	pipeline := &FullPipeline{
		aec:        aec,
		opusCodec:  opus,
		vad:        NewEnergyVAD(),
		ns:         ns,
		sampleRate: 16000,
		frameSize:  960,
		logger:     logger,
		aecEnabled: aecEnabled,
	}

	if aecEnabled {
		logger.Info("full audio pipeline initialized",
			"sample_rate", 16000,
			"frame_size", 960,
			"components", []string{"AEC", "NS", "AGC", "Opus", "VAD"},
		)
	} else {
		logger.Info("audio pipeline initialized (degraded: no AEC)",
			"sample_rate", 16000,
			"frame_size", 960,
			"components", []string{"NS", "AGC", "Opus", "VAD"},
		)
	}

	return pipeline, nil
}

// =============================================================================
// 核心处理方法
// =============================================================================

// ProcessInbound 处理下行音频（TTS 播放前的预处理）。
//
// 将 TTS PCM 数据馈送到 AEC 作为参考信号。
// 必须在发送音频到设备扬声器之前调用。
//
// 参数：
//   - pcm：PCM 16-bit 小端字节数组（1920 bytes = 960 samples）
//
// 返回：
//   - error：AEC 处理错误（降级模式下忽略）
func (p *FullPipeline) ProcessInbound(pcm []byte) error {
	if !p.aecEnabled || p.aec == nil {
		return nil // 降级：旁路
	}

	samples := bytesToSamples(pcm)
	return p.aec.ProcessStream(samples)
}

// ProcessOutbound 处理上行音频（麦克风输入的完整处理）。
//
// 管线：AEC → NS → VAD → Opus 编码
// 返回编码后的 Opus 数据和语音检测结果。
//
// 参数：
//   - pcm：PCM 16-bit 小端字节数组（1920 bytes = 960 samples）
//
// 返回：
//   - opus：Opus 编码数据（可直接发送到 ASR）
//   - isSpeech：VAD 检测结果（true = 语音帧）
//   - error：处理错误
func (p *FullPipeline) ProcessOutbound(pcm []byte) (opus []byte, isSpeech bool, err error) {
	// 验证输入大小
	expectedLen := p.frameSize * 2 // 960 samples × 2 bytes
	if len(pcm) != expectedLen {
		return nil, false, ErrInvalidFrameSize
	}

	samples := bytesToSamples(pcm)

	// 1. 回声消除（AEC：参考信号是下行 TTS）
	if p.aecEnabled && p.aec != nil {
		cleaned, aecErr := p.aec.ProcessReverseStream(samples)
		if aecErr != nil {
			p.logger.Warn("aec process failed, bypassing aec for this frame",
				"error", aecErr,
				"frame_size", len(samples),
			)
			// AEC 失败不中断管线，继续处理原始采样
		} else {
			samples = cleaned
		}
	}

	// 2. 噪声抑制
	if p.ns != nil && p.ns.IsReady() {
		cleanedBytes, nsErr := p.ns.Suppress(samplesToBytes(samples))
		if nsErr != nil {
			p.logger.Warn("ns process failed, bypassing ns for this frame",
				"error", nsErr,
			)
		} else {
			samples = bytesToSamples(cleanedBytes)
		}
	}

	// 3. VAD 语音检测
	cleanedBytes := samplesToBytes(samples)
	isSpeech = p.vad.IsSpeech(cleanedBytes)

	// 4. Opus 编码
	opus, err = p.opusCodec.Encode(cleanedBytes)
	if err != nil {
		return nil, false, err
	}

	return opus, isSpeech, nil
}

// =============================================================================
// 编解码方法（兼容现有 Processor 接口）
// =============================================================================

// DecodeOpus 将 Opus 帧解码为 PCM 字节。
//
// 委托给 OpusCodec.Decode。
// 返回 PCM 16-bit 小端字节数组（1920 bytes）。
func (p *FullPipeline) DecodeOpus(opusFrame []byte) ([]byte, error) {
	return p.opusCodec.Decode(opusFrame)
}

// EncodeOpus 将 PCM 字节编码为 Opus 帧。
//
// 委托给 OpusCodec.Encode。
// 注意：此方法不经过 AEC/NS/VAD 管线，仅做纯 Opus 编码。
// 用于 TTS 音频编码后发送到设备。
func (p *FullPipeline) EncodeOpus(pcm []byte) ([]byte, error) {
	return p.opusCodec.Encode(pcm)
}

// DetectSpeech 检测 PCM 帧中是否包含语音活动。
//
// 委托给 EnergyVAD.IsSpeech。
func (p *FullPipeline) DetectSpeech(pcm []byte) bool {
	return p.vad.IsSpeech(pcm)
}

// =============================================================================
// 生命周期管理
// =============================================================================

// Reset 重置管线状态。
//
// 清空 VAD 上下文（静音/语音帧计数），用于新对话开始。
// AEC 滤波器系数和 NS 噪声估计保留（跨对话收敛）。
func (p *FullPipeline) Reset() {
	if p.vad != nil {
		p.vad.Reset()
	}
}

// Close 释放管线资源。
//
// 关闭 Opus 编解码器，释放 AEC 资源。
func (p *FullPipeline) Close() error {
	if p.opusCodec != nil {
		if err := p.opusCodec.Close(); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// 状态查询
// =============================================================================

// IsAECEnabled 返回 AEC 是否启用。
func (p *FullPipeline) IsAECEnabled() bool {
	return p.aecEnabled
}

// FrameSize 返回每帧采样数（960 samples at 16kHz = 60ms）。
func (p *FullPipeline) FrameSize() int {
	return p.frameSize
}

// SampleRate 返回采样率（16000 Hz）。
func (p *FullPipeline) SampleRate() uint32 {
	return p.sampleRate
}

// OpusCodec 返回底层 Opus 编解码器（用于高级场景）。
func (p *FullPipeline) OpusCodec() *OpusCodec {
	return p.opusCodec
}