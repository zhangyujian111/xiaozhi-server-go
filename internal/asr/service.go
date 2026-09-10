package asr

import "context"

// Service ASR 语音识别服务接口。
//
// 职责：
//   - 音频转文字（同步 + 流式）
//   - 多语言支持
//   - 音频格式兼容
type Service interface {
	// Transcribe 同步语音识别。
	Transcribe(ctx context.Context, req TranscribeReq) (*TranscribeResp, error)

	// TranscribeStream 流式语音识别。
	TranscribeStream(ctx context.Context, stream <-chan []byte, opts StreamOptions) (<-chan *StreamChunk, error)

	// GetLanguages 获取支持的识别语言列表。
	GetLanguages(ctx context.Context) ([]Language, error)

	// GetModels 获取支持的识别模型列表。
	GetModels(ctx context.Context) ([]Model, error)
}