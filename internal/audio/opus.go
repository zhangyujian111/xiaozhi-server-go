package audio

import (
	"encoding/binary"
	"fmt"

	"github.com/hraban/opus"
)

// Codec Opus 编解码器接口。
type Codec interface {
	// Encode 将 PCM 字节编码为 Opus 帧。
	Encode(pcm []byte) ([]byte, error)

	// Decode 将 Opus 帧解码为 PCM 字节。
	Decode(opusFrame []byte) ([]byte, error)

	// Close 释放编解码器资源。
	Close() error
}

// OpusCodec Opus 编解码器实现。
//
// 使用 github.com/hraban/opus 绑定 libopus。
// 编解码参数：
//   - 采样率：16kHz
//   - 声道：单声道
//   - 帧长：60ms（960 samples）
//   - 应用类型：VoIP
type OpusCodec struct {
	encoder    *opus.Encoder
	decoder    *opus.Decoder
	sampleRate int // 16000
	channels   int // 1
	frameSize  int // 960 samples per frame
}

// NewOpusCodec 创建 Opus 编解码器。
//
// 采样率 16kHz，单声道，VoIP 优化。
// 系统依赖：CentOS 需安装 opus-devel，Debian 需安装 libopus-dev。
func NewOpusCodec() (*OpusCodec, error) {
	enc, err := opus.NewEncoder(16000, 1, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("audio: create opus encoder: %w", err)
	}
	dec, err := opus.NewDecoder(16000, 1)
	if err != nil {
		return nil, fmt.Errorf("audio: create opus decoder: %w", err)
	}
	return &OpusCodec{
		encoder:    enc,
		decoder:    dec,
		sampleRate: 16000,
		channels:   1,
		frameSize:  960,
	}, nil
}

// Encode 将 PCM 字节编码为 Opus 帧。
//
// pcm 长度必须是 frameSize * channels * 2 = 1920 bytes（960 samples × 16-bit × 1 channel）。
// 返回 Opus 编码数据（通常 < 200 bytes for 16kbps）。
func (c *OpusCodec) Encode(pcm []byte) ([]byte, error) {
	expectedLen := c.frameSize * c.channels * 2 // 1920 bytes
	if len(pcm) != expectedLen {
		return nil, fmt.Errorf("%w: got %d bytes, expected %d", ErrInvalidFrameSize, len(pcm), expectedLen)
	}

	// 将 PCM 字节转换为 int16 采样数组
	pcmSamples := make([]int16, len(pcm)/2)
	for i := range pcmSamples {
		pcmSamples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}

	// Opus 编码
	opusBuf := make([]byte, 4000) // 最大 Opus 帧大小（1275 bytes + 安全余量）
	n, err := c.encoder.Encode(pcmSamples, opusBuf)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpusEncodeFailed, err)
	}
	return opusBuf[:n], nil
}

// Decode 将 Opus 帧解码为 PCM 字节。
//
// opusFrame 为 Opus 编码数据。
// 返回 PCM 16-bit 小端字节数组（1920 bytes = 960 samples × 2 bytes）。
func (c *OpusCodec) Decode(opusFrame []byte) ([]byte, error) {
	pcmSamples := make([]int16, c.frameSize)
	n, err := c.decoder.Decode(opusFrame, pcmSamples)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOpusDecodeFailed, err)
	}

	// 将 int16 采样数组转换为 PCM 字节
	pcmBuf := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(pcmBuf[i*2:], uint16(pcmSamples[i]))
	}
	return pcmBuf, nil
}

// Close 释放编解码器资源。
func (c *OpusCodec) Close() error {
	// hraban/opus 的 Encoder/Decoder 没有显式 Close 方法，
	// 底层 libopus 在 GC 回收时自动释放。
	return nil
}

// FrameSize 返回每帧采样数。
func (c *OpusCodec) FrameSize() int {
	return c.frameSize
}

// SampleRate 返回采样率。
func (c *OpusCodec) SampleRate() int {
	return c.sampleRate
}