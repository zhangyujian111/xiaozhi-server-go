package tts

import (
	"context"
	"fmt"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
)

const maxTextLength = 500

// aisaasTTSService 委托 aisaas SDK 的 TTS 服务实现。
type aisaasTTSService struct {
	client *aisaas.Client
}

// NewService 创建委托 aisaas SDK 的 TTS 服务。
func NewService(client *aisaas.Client) Service {
	return &aisaasTTSService{client: client}
}

// Synthesize 同步语音合成。
func (s *aisaasTTSService) Synthesize(ctx context.Context, req SynthReq) (*SynthResp, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req.Text == "" {
		return nil, fmt.Errorf("tts: text is empty")
	}
	if len([]rune(req.Text)) > maxTextLength {
		return nil, fmt.Errorf("%w: max %d characters", ErrTextTooLong, maxTextLength)
	}

	voice := req.Voice
	if voice == "" {
		voice = "default"
	}
	format := req.Format
	if format == "" {
		format = "opus"
	}

	// 委托 aisaas SDK
	audio, err := s.client.TTS(ctx, req.Text, voice, format)
	if err != nil {
		return nil, fmt.Errorf("tts: %w", err)
	}

	return &SynthResp{
		Audio:      audio,
		Format:     format,
		Duration:   float64(len(audio)) / 24000.0, // 估算：24kHz 采样率
		CharCount:  len([]rune(req.Text)),
		SampleRate: 24000,
	}, nil
}

// SynthesizeStream 流式语音合成。
func (s *aisaasTTSService) SynthesizeStream(ctx context.Context, req SynthReq) (<-chan *SynthChunk, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req.Text == "" {
		return nil, fmt.Errorf("tts: text is empty")
	}

	voice := req.Voice
	if voice == "" {
		voice = "default"
	}
	format := req.Format
	if format == "" {
		format = "opus"
	}

	out := make(chan *SynthChunk, 10)

	go func() {
		defer close(out)

		seq := 0
		err := s.client.TTSStream(ctx, req.Text, voice, format, func(chunk []byte) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- &SynthChunk{
				Audio:    chunk,
				Format:   format,
				IsFinal:  false,
				Sequence: seq,
			}:
				seq++
				return nil
			}
		})

		// 发送最终块
		select {
		case <-ctx.Done():
			return
		default:
			if err == nil {
				out <- &SynthChunk{
					Audio:    nil,
					Format:   format,
					IsFinal:  true,
					Sequence: seq,
				}
			}
		}
	}()

	return out, nil
}

// GetVoices 获取支持的音色列表。
func (s *aisaasTTSService) GetVoices(ctx context.Context) ([]Voice, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return []Voice{
		{ID: "default", Name: "默认音色", Language: "zh-CN", Gender: "female", Description: "默认女声", IsDefault: true},
		{ID: "male-01", Name: "男声一号", Language: "zh-CN", Gender: "male", Description: "沉稳男声", IsDefault: false},
		{ID: "female-01", Name: "女声一号", Language: "zh-CN", Gender: "female", Description: "温柔女声", IsDefault: false},
		{ID: "en-male-01", Name: "English Male", Language: "en-US", Gender: "male", Description: "Professional male voice", IsDefault: false},
		{ID: "en-female-01", Name: "English Female", Language: "en-US", Gender: "female", Description: "Warm female voice", IsDefault: false},
	}, nil
}

// GetFormats 获取支持的音频格式列表。
func (s *aisaasTTSService) GetFormats(ctx context.Context) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return []string{"pcm", "opus", "mp3", "wav", "aac", "flac"}, nil
}