package aisaas

import (
	"context"
	"fmt"
	"io"
)

// ============================================================
// TTS（文字转语音）
// ============================================================

// TTS 文字转语音。
//
// POST /v1/audio/speech
// 鉴权: Bearer API Key
//
// 返回音频二进制数据（opaque bytes）。
// 默认 voice 为 "default"，format 为 "mp3"。
func (c *Client) TTS(ctx context.Context, text string, voice string, format string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("aisaas: tts text is empty")
	}
	if voice == "" {
		voice = "default"
	}
	if format == "" {
		format = "mp3"
	}

	req := TTSRequest{
		Model:          "tts-1",
		Input:          text,
		Voice:          voice,
		ResponseFormat: format,
	}

	body, _, err := c.doRequest(ctx, "POST", "/v1/audio/speech", req, "bearer", nil)
	if err != nil {
		return nil, fmt.Errorf("aisaas: tts: %w", err)
	}

	return body, nil
}

// TTSStream 流式 TTS。
//
// POST /v1/audio/speech（流式响应）
// 鉴权: Bearer API Key
//
// 流式返回音频块，每块通过 callback 回调。
func (c *Client) TTSStream(ctx context.Context, text string, voice string, format string, callback func(chunk []byte) error) error {
	if text == "" {
		return fmt.Errorf("aisaas: tts stream text is empty")
	}
	if callback == nil {
		return fmt.Errorf("aisaas: tts stream callback is nil")
	}
	if voice == "" {
		voice = "default"
	}
	if format == "" {
		format = "mp3"
	}

	req := TTSRequest{
		Model:          "tts-1",
		Input:          text,
		Voice:          voice,
		ResponseFormat: format,
	}

	resp, err := c.doRequestRaw(ctx, "POST", "/v1/audio/speech", req, "bearer", nil)
	if err != nil {
		return fmt.Errorf("aisaas: tts stream: %w", err)
	}
	defer resp.Body.Close()

	// 流式读取音频数据
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if cbErr := callback(chunk); cbErr != nil {
				return fmt.Errorf("aisaas: tts stream callback error: %w", cbErr)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("aisaas: tts stream read: %w", err)
		}
	}

	return nil
}
