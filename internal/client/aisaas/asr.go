package aisaas

import (
	"context"
	"fmt"
)

// ============================================================
// ASR（语音识别）
// ============================================================

// ASR 语音识别。
//
// POST /v1/audio/transcriptions
// 鉴权: Bearer API Key
//
// 上传音频二进制数据（multipart/form-data），返回识别文本。
// 默认 format 为 "opus"，language 为空（自动检测）。
func (c *Client) ASR(ctx context.Context, audio []byte, format string, language string) (*ASRResponse, error) {
	if len(audio) == 0 {
		return nil, fmt.Errorf("aisaas: asr audio is empty")
	}
	if format == "" {
		format = "opus"
	}

	fields := map[string]string{
		"model":           "whisper-1",
		"response_format": "json",
	}
	if language != "" {
		fields["language"] = language
	}

	fileName := fmt.Sprintf("audio.%s", format)
	var resp ASRResponse
	_, _, err := c.doRequestMultipart(ctx, "/v1/audio/transcriptions", fields, "file", fileName, audio, &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: asr: %w", err)
	}

	return &resp, nil
}
