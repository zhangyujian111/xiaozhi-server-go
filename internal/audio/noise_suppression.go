// Package audio 提供独立的噪声抑制 + 自动增益控制。
//
// 当不需要完整 AEC 管线时，可使用 NoiseSuppressor 仅做降噪和增益控制。
// 内部复用 AECProcessor 的 NS + AGC 部分（ProcessReverseStream 管线）。
package audio

// NoiseSuppressor 噪声抑制器。
//
// 提供独立的 NS + AGC 处理，不包含 AEC（回声消除）。
// 适用于：
//   - 仅需降噪的场景（如 TTS 后处理）
//   - 不需要回声消除的音频流（如耳机模式）
//   - 对 AECProcessor 降噪能力的独立封装
type NoiseSuppressor struct {
	processor *AECProcessor
}

// NewNoiseSuppressor 创建噪声抑制器。
//
// 内部创建 AECProcessor（16kHz, 单声道），但仅使用其 NS + AGC 功能。
// 注意：AECProcessor 的构造函数已配置 AEC + NS + AGC，但 AEC 需要参考信号才有意义。
// 在无参考信号的场景下，AEC 部分自动旁路。
func NewNoiseSuppressor() *NoiseSuppressor {
	proc, err := NewAECProcessor(16000, 1)
	if err != nil {
		// 构造函数不会返回错误（配置已验证），安全兜底
		return &NoiseSuppressor{processor: nil}
	}
	return &NoiseSuppressor{processor: proc}
}

// Suppress 对 PCM 数据应用噪声抑制和自动增益控制。
//
// 参数：
//   - pcm：PCM 16-bit 小端字节数组（1920 bytes = 960 samples at 16kHz）
//
// 返回：
//   - []byte：降噪后的 PCM 数据（同长度）
//   - error：处理错误
func (n *NoiseSuppressor) Suppress(pcm []byte) ([]byte, error) {
	if n.processor == nil {
		return pcm, ErrAECNotReady
	}

	samples := bytesToSamples(pcm)
	cleaned, err := n.processor.ProcessReverseStream(samples)
	if err != nil {
		return nil, err
	}
	return samplesToBytes(cleaned), nil
}

// IsReady 返回噪声抑制器是否可用。
func (n *NoiseSuppressor) IsReady() bool {
	return n.processor != nil
}