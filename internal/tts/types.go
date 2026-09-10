// Package tts 语音合成封装服务。
//
// 职责：TTS 服务封装，通过 ykt-aisaas SDK 调用语音合成能力。
// 实现：委托 aisaas.Client.TTS() / TTSStream()。
package tts

import "errors"

// SynthReq 语音合成请求。
type SynthReq struct {
	Text       string  `json:"text"`       // 待合成文本（必填）
	Voice      string  `json:"voice"`      // 音色 ID（默认 "default"）
	Format     string  `json:"format"`     // 输出格式：pcm / opus / mp3 / wav（默认 opus）
	Speed      float64 `json:"speed"`      // 语速（0.5-2.0，默认 1.0）
	Pitch      float64 `json:"pitch"`      // 音调（-12.0~12.0，默认 0.0）
	Volume     float64 `json:"volume"`     // 音量（0.0-1.0，默认 1.0）
	SampleRate int     `json:"sampleRate"` // 采样率（默认 24000）
	Model      string  `json:"model"`      // 合成模型（默认使用 aisaas 配置）
}

// SynthResp 语音合成响应。
type SynthResp struct {
	Audio      []byte  `json:"-"`          // 音频数据（不序列化到 JSON）
	Format     string  `json:"format"`     // 音频格式
	Duration   float64 `json:"duration"`   // 音频时长（秒）
	CharCount  int     `json:"charCount"`  // 合成字符数
	SampleRate int     `json:"sampleRate"` // 采样率
}

// SynthChunk 流式合成数据块。
type SynthChunk struct {
	Audio    []byte `json:"-"`       // 音频数据块
	Format   string `json:"format"`  // 音频格式
	IsFinal  bool   `json:"isFinal"` // 是否最后一块
	Sequence int    `json:"sequence"` // 块序号（从 0 开始）
}

// Voice 音色信息。
type Voice struct {
	ID          string `json:"id"`          // 音色 ID
	Name        string `json:"name"`        // 音色名称
	Language    string `json:"language"`    // 支持语言
	Gender      string `json:"gender"`      // 性别：male / female / neutral
	Description string `json:"description"` // 描述
	SampleURL   string `json:"sampleUrl"`   // 试听 URL
	IsDefault   bool   `json:"isDefault"`   // 是否默认音色
}

// 哨兵错误。
var (
	ErrTTSUnavailable = errors.New("tts: service unavailable")
	ErrTTSTimeout     = errors.New("tts: synthesis timeout")
	ErrTextTooLong    = errors.New("tts: text exceeds maximum length")
	ErrInvalidVoice   = errors.New("tts: voice not found")
)