package asr

import (
	"context"
	"fmt"

	"github.com/ykt/xiaozhi-server-go/internal/client/aisaas"
)

// aisaasASRService 委托 aisaas SDK 的 ASR 服务实现。
type aisaasASRService struct {
	client *aisaas.Client
}

// NewService 创建委托 aisaas SDK 的 ASR 服务。
func NewService(client *aisaas.Client) Service {
	return &aisaasASRService{client: client}
}

// Transcribe 同步语音识别。
func (s *aisaasASRService) Transcribe(ctx context.Context, req TranscribeReq) (*TranscribeResp, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(req.Audio) == 0 {
		return nil, fmt.Errorf("%w: audio is empty", ErrInvalidAudio)
	}

	format := req.Format
	if format == "" {
		format = "opus"
	}

	// 委托 aisaas SDK
	result, err := s.client.ASR(ctx, req.Audio, format, req.Language)
	if err != nil {
		return nil, fmt.Errorf("asr: %w", err)
	}

	return &TranscribeResp{
		Text:       result.Text,
		Language:   result.Language,
		Confidence: 0.95, // SDK 未返回置信度，使用默认值
		Duration:   result.Duration,
	}, nil
}

// TranscribeStream 流式语音识别。
//
// 将音频流收集后调用同步 ASR，返回单个识别结果。
// 适用于不支持实时流式识别的场景。
func (s *aisaasASRService) TranscribeStream(ctx context.Context, stream <-chan []byte, opts StreamOptions) (<-chan *StreamChunk, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stream == nil {
		return nil, fmt.Errorf("asr: stream is nil")
	}

	out := make(chan *StreamChunk, 1)

	go func() {
		defer close(out)

		// 收集所有音频帧
		var audio []byte
		for chunk := range stream {
			select {
			case <-ctx.Done():
				return
			default:
				audio = append(audio, chunk...)
			}
		}

		if len(audio) == 0 {
			return
		}

		format := opts.Format
		if format == "" {
			format = "opus"
		}

		// 委托 aisaas SDK
		result, err := s.client.ASR(ctx, audio, format, opts.Language)
		if err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case out <- &StreamChunk{
			Text:       result.Text,
			IsFinal:    true,
			Confidence: 0.95,
		}:
		}
	}()

	return out, nil
}

// GetLanguages 获取支持的识别语言列表。
func (s *aisaasASRService) GetLanguages(ctx context.Context) ([]Language, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return []Language{
		{Code: "zh-CN", Name: "中文（简体）"},
		{Code: "zh-TW", Name: "中文（繁体）"},
		{Code: "en-US", Name: "English (US)"},
		{Code: "ja-JP", Name: "日本語"},
		{Code: "ko-KR", Name: "한국어"},
	}, nil
}

// GetModels 获取支持的识别模型列表。
func (s *aisaasASRService) GetModels(ctx context.Context) ([]Model, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return []Model{
		{ID: "whisper-1", Name: "Whisper v1", Description: "通用多语言语音识别模型", MaxDuration: 60},
		{ID: "whisper-2", Name: "Whisper v2", Description: "增强版多语言语音识别模型", MaxDuration: 120},
	}, nil
}