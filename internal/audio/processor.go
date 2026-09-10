package audio

// Processor 音频处理器接口。
//
// 统一封装 Opus 编解码和 VAD 语音检测，提供简洁的音频处理入口。
type Processor interface {
	// EncodeOpus 将 PCM 编码为 Opus（PCM → Opus）。
	EncodeOpus(pcm []byte) ([]byte, error)

	// DecodeOpus 将 Opus 解码为 PCM（Opus → PCM）。
	DecodeOpus(opus []byte) ([]byte, error)

	// DetectSpeech 检测 PCM 帧中是否包含语音活动。
	DetectSpeech(pcm []byte) bool

	// Reset 重置处理器状态（清空 VAD 上下文）。
	Reset()

	// Close 释放处理器资源。
	Close() error
}

// AudioProcessor 音频处理器实现。
//
// 组合 Opus 编解码器和 VAD 检测器，提供统一的音频处理接口。
//
// 使用示例：
//
//	proc, err := audio.NewProcessor()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer proc.Close()
//
//	pcm, err := proc.DecodeOpus(opusFrame)
//	if err != nil {
//	    log.Warn("decode failed", "error", err)
//	    return
//	}
//	if proc.DetectSpeech(pcm) {
//	    // 处理语音帧
//	}
type AudioProcessor struct {
	codec *OpusCodec
	vad   *EnergyVAD
}

// NewProcessor 创建音频处理器。
//
// 初始化 Opus 编解码器（16kHz, 单声道, VoIP）和基于能量的 VAD 检测器。
// 返回错误表示 libopus 初始化失败（需检查系统是否安装 opus-devel/libopus-dev）。
func NewProcessor() (*AudioProcessor, error) {
	codec, err := NewOpusCodec()
	if err != nil {
		return nil, err
	}
	return &AudioProcessor{
		codec: codec,
		vad:   NewEnergyVAD(),
	}, nil
}

// EncodeOpus 将 PCM 编码为 Opus。
//
// pcm 必须是 1920 bytes（960 samples × 16-bit × 1 channel）。
func (p *AudioProcessor) EncodeOpus(pcm []byte) ([]byte, error) {
	return p.codec.Encode(pcm)
}

// DecodeOpus 将 Opus 解码为 PCM。
//
// 返回 PCM 16-bit 小端字节数组（1920 bytes）。
func (p *AudioProcessor) DecodeOpus(opus []byte) ([]byte, error) {
	return p.codec.Decode(opus)
}

// DetectSpeech 检测 PCM 帧中是否包含语音活动。
//
// 使用基于 RMS 能量的 VAD 检测，带迟滞避免频繁切换。
func (p *AudioProcessor) DetectSpeech(pcm []byte) bool {
	return p.vad.IsSpeech(pcm)
}

// Reset 重置处理器状态。
//
// 清空 VAD 上下文（静音/语音帧计数），用于新对话开始。
func (p *AudioProcessor) Reset() {
	p.vad.Reset()
}

// Close 释放处理器资源。
func (p *AudioProcessor) Close() error {
	return p.codec.Close()
}

// FrameSize 返回每帧采样数（960 samples at 16kHz = 60ms）。
func (p *AudioProcessor) FrameSize() int {
	return p.codec.FrameSize()
}

// SampleRate 返回采样率（16000 Hz）。
func (p *AudioProcessor) SampleRate() int {
	return p.codec.SampleRate()
}