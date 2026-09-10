// Package asr 语音识别封装服务。
//
// 职责：ASR 服务封装，通过 ykt-aisaas SDK 调用语音识别能力。
// 实现：委托 aisaas.Client.ASR()。
package asr

import "errors"

// TranscribeReq 同步识别请求。
type TranscribeReq struct {
	Audio      []byte   `json:"-"`          // 音频数据（不序列化到 JSON）
	Format     string   `json:"format"`     // 音频格式：pcm / opus / wav / mp3
	SampleRate int      `json:"sampleRate"` // 采样率（Hz），如 16000
	Language   string   `json:"language"`   // 语言代码：zh-CN / en-US / ja-JP
	Model      string   `json:"model"`      // 识别模型（默认 whisper-1）
	HotWords   []string `json:"hotWords"`   // 热词列表（提升识别准确率）
}

// TranscribeResp 同步识别响应。
type TranscribeResp struct {
	Text       string  `json:"text"`       // 识别文本
	Language   string  `json:"language"`   // 检测到的语言
	Confidence float64 `json:"confidence"` // 置信度（0.0-1.0）
	Duration   float64 `json:"duration"`   // 音频时长（秒）
	Words      []Word  `json:"words"`      // 词级时间戳（可选）
}

// Word 词级识别结果。
type Word struct {
	Word       string  `json:"word"`       // 词语
	Start      float64 `json:"start"`      // 开始时间（秒）
	End        float64 `json:"end"`        // 结束时间（秒）
	Confidence float64 `json:"confidence"` // 置信度
}

// StreamOptions 流式识别选项。
type StreamOptions struct {
	Format     string   `json:"format"`     // 音频格式
	SampleRate int      `json:"sampleRate"` // 采样率
	Language   string   `json:"language"`   // 语言
	Model      string   `json:"model"`      // 识别模型
	Interim    bool     `json:"interim"`    // 是否返回中间结果
	HotWords   []string `json:"hotWords"`   // 热词列表
}

// StreamChunk 流式识别结果块。
type StreamChunk struct {
	Text       string  `json:"text"`       // 当前识别文本片段
	IsFinal    bool    `json:"isFinal"`    // 是否为最终结果
	Confidence float64 `json:"confidence"` // 置信度
	StartTime  float64 `json:"startTime"`  // 片段起始时间（秒）
	EndTime    float64 `json:"endTime"`    // 片段结束时间（秒）
}

// Language 支持的语言。
type Language struct {
	Code string `json:"code"` // 语言代码（zh-CN）
	Name string `json:"name"` // 语言名称（中文（简体））
}

// Model 支持的模型。
type Model struct {
	ID          string `json:"id"`          // 模型 ID
	Name        string `json:"name"`        // 模型名称
	Description string `json:"description"` // 描述
	MaxDuration int    `json:"maxDuration"` // 最大支持时长（秒）
}

// 哨兵错误。
var (
	ErrASRUnavailable = errors.New("asr: service unavailable")
	ErrASRTimeout     = errors.New("asr: recognition timeout")
	ErrInvalidAudio   = errors.New("asr: invalid audio format")
	ErrNoSpeech       = errors.New("asr: no speech detected")
)