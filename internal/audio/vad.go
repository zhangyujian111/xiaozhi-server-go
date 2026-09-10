package audio

import (
	"encoding/binary"
	"math"
	"time"
)

// VAD 语音活动检测器接口。
type VAD interface {
	// IsSpeech 检测 PCM 字节中是否包含语音活动。
	//
	// 返回 true 表示检测到人声，false 表示静音。
	IsSpeech(pcm []byte) bool

	// Reset 重置 VAD 状态（用于新对话开始时清空上下文）。
	Reset()
}

// EnergyVAD 基于 RMS 能量检测的 VAD 实现。
//
// 计算 PCM 帧的均方根能量，通过阈值判断是否包含语音。
// 使用迟滞（hysteresis）机制避免频繁切换：
//   - 语音开始：连续 3 帧（60ms）能量高于阈值
//   - 语音结束：连续 10 帧（200ms）能量低于阈值
//
// 每帧参数：
//   - 帧长：20ms（320 samples at 16kHz）
//   - 阈值：500（RMS）
type EnergyVAD struct {
	threshold     float64       // 能量阈值（默认 500）
	silenceFrames int           // 连续静音帧数
	speechFrames  int           // 连续语音帧数
	isSpeech      bool          // 当前语音状态
	frameDuration time.Duration // 20ms per frame
}

// NewEnergyVAD 创建基于能量的 VAD 检测器。
//
// 默认阈值 500 RMS，适用于近距离麦克风场景。
// 安静环境可降低至 200，嘈杂环境可提高至 1000。
func NewEnergyVAD() *EnergyVAD {
	return &EnergyVAD{
		threshold:     500,
		silenceFrames: 0,
		speechFrames:  0,
		isSpeech:      false,
		frameDuration: 20 * time.Millisecond,
	}
}

// NewEnergyVADWithThreshold 创建自定义阈值的 VAD 检测器。
func NewEnergyVADWithThreshold(threshold float64) *EnergyVAD {
	v := NewEnergyVAD()
	v.threshold = threshold
	return v
}

// IsSpeech 检测 PCM 字节中是否包含语音活动。
//
// 实现迟滞逻辑：
//   - 如果能量 > 阈值：增加语音计数，连续 3 帧语音则判定为"语音"
//   - 如果能量 ≤ 阈值：增加静音计数，连续 10 帧静音则判定为"静音"
//   - 中间状态保持当前判定不变
func (v *EnergyVAD) IsSpeech(pcm []byte) bool {
	// 计算 RMS 能量
	rms := 0.0
	samples := len(pcm) / 2
	if samples == 0 {
		// 空帧，视为静音
		v.silenceFrames++
		v.speechFrames = 0
		if v.silenceFrames >= 10 {
			v.isSpeech = false
		}
		return v.isSpeech
	}

	for i := 0; i < samples; i++ {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		rms += sample * sample
	}
	rms = math.Sqrt(rms / float64(samples))

	if rms > v.threshold {
		v.speechFrames++
		v.silenceFrames = 0
		if v.speechFrames >= 3 { // 60ms 持续语音
			v.isSpeech = true
		}
	} else {
		v.silenceFrames++
		v.speechFrames = 0
		if v.silenceFrames >= 10 { // 200ms 持续静音
			v.isSpeech = false
		}
	}

	return v.isSpeech
}

// Reset 重置 VAD 状态。
//
// 清空静音/语音帧计数和当前状态，用于新对话开始时。
func (v *EnergyVAD) Reset() {
	v.silenceFrames = 0
	v.speechFrames = 0
	v.isSpeech = false
}

// Threshold 返回当前能量阈值。
func (v *EnergyVAD) Threshold() float64 {
	return v.threshold
}

// IsActive 返回当前是否处于语音状态。
func (v *EnergyVAD) IsActive() bool {
	return v.isSpeech
}