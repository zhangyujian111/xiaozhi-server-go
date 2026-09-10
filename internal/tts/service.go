package tts

import "context"

// Service TTS 语音合成服务接口。
//
// 职责：
//   - 文字转语音（同步 + 流式）
//   - 多音色支持
//   - 语速/音调调节
type Service interface {
	// Synthesize 同步语音合成。
	Synthesize(ctx context.Context, req SynthReq) (*SynthResp, error)

	// SynthesizeStream 流式语音合成。
	SynthesizeStream(ctx context.Context, req SynthReq) (<-chan *SynthChunk, error)

	// GetVoices 获取支持的音色列表。
	GetVoices(ctx context.Context) ([]Voice, error)

	// GetFormats 获取支持的音频格式列表。
	GetFormats(ctx context.Context) ([]string, error)
}