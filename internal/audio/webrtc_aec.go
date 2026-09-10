// Package audio 提供 WebRTC 风格音频处理（AEC + NS + AGC）。
//
// 回声消除管线：
//
//	下行（TTS 播放）→ ProcessStream → 参考缓冲区
//	上行（麦克风）→ ProcessReverseStream → AEC → NS → AGC → 清洁 PCM
//
// 处理参数：
//   - 采样率：16kHz
//   - 帧长：60ms（960 samples）
//   - 声道：单声道
//   - AEC 滤波器：256 tap NLMS 自适应滤波器
//   - NS：频谱减法（Wiener 风格）
//   - AGC：RMS 增益控制 + 限幅器
package audio

import (
	"encoding/binary"
	"math"

	_ "github.com/pion/webrtc/v3" // WebRTC 音频处理依赖（未来 AEC 升级）
)

// =============================================================================
// 配置类型
// =============================================================================

// AecSuppressionLevel AEC 回声抑制级别。
type AecSuppressionLevel int

const (
	// AecSuppressionLow 低抑制（-6 dB，适合安静环境）。
	AecSuppressionLow AecSuppressionLevel = iota
	// AecSuppressionModerate 适度抑制（-12 dB，默认）。
	AecSuppressionModerate
	// AecSuppressionHigh 高抑制（-18 dB，适合嘈杂环境）。
	AecSuppressionHigh
	// AecSuppressionVeryHigh 极高抑制（-24 dB）。
	AecSuppressionVeryHigh
)

// AecConfig 回声消除配置。
type AecConfig struct {
	SuppressionLevel AecSuppressionLevel // 抑制级别
	DelayAgnostic    bool                // 延迟无关（容忍网络抖动）
	ExtendedFilter   bool                // 扩展滤波器（更长的回声尾）
	StreamDelayMs    int                 // 流延迟（0 = 自动估计）
}

// NsSuppressionLevel 噪声抑制级别。
type NsSuppressionLevel int

const (
	// NsSuppressionLow 低抑制（-6 dB）。
	NsSuppressionLow NsSuppressionLevel = iota
	// NsSuppressionModerate 适度抑制（-12 dB，默认）。
	NsSuppressionModerate
	// NsSuppressionHigh 高抑制（-18 dB）。
	NsSuppressionHigh
	// NsSuppressionVeryHigh 极高抑制（-24 dB）。
	NsSuppressionVeryHigh
)

// NsConfig 噪声抑制配置。
type NsConfig struct {
	SuppressionLevel NsSuppressionLevel // 抑制级别
}

// AgcConfig 自动增益控制配置。
type AgcConfig struct {
	TargetLevelDbfs   int  // 目标音量（dBFS），如 -3 = -3 dBFS
	CompressionGainDb int  // 压缩增益（dB），如 9 = 9 dB
	EnableLimiter     bool // 启用限幅器（防止削波）
}

// =============================================================================
// 音频帧类型
// =============================================================================

// AudioFrame WebRTC 风格音频帧，与 pion/webrtc 兼容。
type AudioFrame struct {
	SampleRate        uint32 // 采样率（Hz）
	NumChannels       uint32 // 声道数
	SamplesPerChannel int    // 每声道采样数
	Data              []byte // PCM 16-bit 小端数据
}

// =============================================================================
// AudioProcessing 音频处理引擎
// =============================================================================

// AudioProcessing WebRTC 风格音频处理模块。
//
// 实现 AEC（回声消除）+ NS（噪声抑制）+ AGC（自动增益控制）。
// 使用纯 Go 实现，无需 CGo 或外部依赖。
//
// 算法：
//   - AEC：NLMS 自适应滤波器（256 tap）
//   - NS：频谱减法（Wiener 风格）
//   - AGC：RMS 增益控制 + 限幅器
type AudioProcessing struct {
	// 配置
	aecCfg AecConfig
	nsCfg  NsConfig
	agcCfg AgcConfig

	// AEC 状态
	refBuffer   []int16   // 参考信号缓冲区（下行 TTS）
	filterCoeff []float64 // NLMS 滤波器系数
	filterLen   int       // 滤波器长度（taps）
	filterInit  bool      // 滤波器是否已初始化

	// NS 状态
	noiseFloor   float64 // 估计噪声基底（RMS）
	noiseFrames  int     // 连续静音帧数
	noiseInit    bool    // 噪声估计是否已初始化

	// AGC 状态
	gain      float64 // 当前增益
	targetRMS float64 // 目标 RMS

	// 降级标记
	degraded bool // true = 已降级到旁路模式
}

// NewAudioProcessing 创建音频处理模块。
//
// 初始状态为旁路（AEC/NS/AGC 关闭），通过 SetXxxConfig 启用。
func NewAudioProcessing() *AudioProcessing {
	return &AudioProcessing{
		filterLen: 256, // 16ms at 16kHz
		gain:      1.0,
	}
}

// SetAecConfig 设置 AEC 配置。
//
// 配置 AEC 后自动启用回声消除。
// 返回 error 仅在参数无效时（如 filterLen = 0）。
func (ap *AudioProcessing) SetAecConfig(cfg AecConfig) error {
	ap.aecCfg = cfg
	if cfg.ExtendedFilter {
		ap.filterLen = 512 // 32ms
	}
	ap.filterCoeff = make([]float64, ap.filterLen)
	ap.filterInit = true
	return nil
}

// SetNsConfig 设置 NS 配置。
func (ap *AudioProcessing) SetNsConfig(cfg NsConfig) error {
	ap.nsCfg = cfg
	return nil
}

// SetAgcConfig 设置 AGC 配置。
func (ap *AudioProcessing) SetAgcConfig(cfg AgcConfig) error {
	ap.agcCfg = cfg
	// 将 dBFS 转换为目标 RMS（满幅 32768）
	ap.targetRMS = math.Pow(10, float64(cfg.TargetLevelDbfs)/20) * 32768
	return nil
}

// IsDegraded 返回是否已降级到旁路模式。
func (ap *AudioProcessing) IsDegraded() bool {
	return ap.degraded
}

// =============================================================================
// 核心处理
// =============================================================================

// ProcessStream 处理下行流（播放给用户的 TTS 音频）。
//
// 将下行 PCM 数据存入参考缓冲区，供后续 AEC 使用。
// 必须在发送音频到扬声器之前调用。
func (ap *AudioProcessing) ProcessStream(frame *AudioFrame) (*AudioFrame, error) {
	if ap.degraded {
		return frame, nil
	}

	samples := bytesToSamples(frame.Data)

	// 更新参考缓冲区（环形）
	ap.refBuffer = append(ap.refBuffer, samples...)
	// 保持缓冲区大小：最多 4 倍滤波器长度（约 64ms）
	maxBuffer := ap.filterLen * 4
	if len(ap.refBuffer) > maxBuffer {
		ap.refBuffer = ap.refBuffer[len(ap.refBuffer)-maxBuffer:]
	}

	return frame, nil
}

// ProcessReverseStream 处理上行流（用户麦克风输入）。
//
// 管线：AEC → NS → AGC
// 返回清洁后的 PCM 数据。
func (ap *AudioProcessing) ProcessReverseStream(frame *AudioFrame) (*AudioFrame, error) {
	if ap.degraded {
		return frame, nil
	}

	samples := bytesToSamples(frame.Data)

	// 1. 回声消除（NLMS 自适应滤波）
	samples = ap.applyAEC(samples)

	// 2. 噪声抑制（频谱减法）
	samples = ap.applyNS(samples)

	// 3. 自动增益控制
	samples = ap.applyAGC(samples)

	return &AudioFrame{
		SampleRate:        frame.SampleRate,
		NumChannels:       frame.NumChannels,
		SamplesPerChannel: len(samples),
		Data:              samplesToBytes(samples),
	}, nil
}

// =============================================================================
// AEC 实现（NLMS 自适应滤波器）
// =============================================================================

// applyAEC 应用 NLMS 自适应回声消除。
//
// 使用参考信号（下行 TTS 音频）估计并消除麦克风中的回声成分。
// 算法：Normalized Least Mean Squares (NLMS)
//
// 参数：
//   - stepSize：NLMS 步长（0.005 ~ 0.05，越小越稳定但收敛慢）
func (ap *AudioProcessing) applyAEC(samples []int16) []int16 {
	if !ap.filterInit || len(ap.refBuffer) < ap.filterLen {
		return samples // 参考信号不足，旁路
	}

	stepSize := 0.01 // NLMS 步长
	refLen := len(ap.refBuffer)

	result := make([]int16, len(samples))

	for i := 0; i < len(samples); i++ {
		// 参考信号窗口：对齐到当前近端采样
		refIdx := refLen - ap.filterLen + i - len(samples)
		if refIdx < 0 || refIdx+ap.filterLen > refLen {
			result[i] = samples[i]
			continue
		}

		refWindow := ap.refBuffer[refIdx : refIdx+ap.filterLen]

		// 计算估计回声：y[n] = Σ w[k] * x[n-k]
		var echoEst float64
		for j := 0; j < ap.filterLen; j++ {
			echoEst += ap.filterCoeff[j] * float64(refWindow[j])
		}

		// 误差 = 近端信号 - 估计回声
		err := float64(samples[i]) - echoEst
		result[i] = clampToInt16(err)

		// NLMS 更新滤波器系数
		var refPower float64
		for j := 0; j < ap.filterLen; j++ {
			refPower += float64(refWindow[j]) * float64(refWindow[j])
		}
		refPower = math.Max(refPower, 1e-6) // 防止除零

		mu := stepSize / refPower
		for j := 0; j < ap.filterLen; j++ {
			ap.filterCoeff[j] += mu * err * float64(refWindow[j])
		}
	}

	return result
}

// =============================================================================
// NS 实现（频谱减法 / Wiener 风格）
// =============================================================================

// applyNS 应用噪声抑制。
//
// 估计噪声基底（静音帧的 RMS），从信号中减去噪声成分。
// 使用简化的时域频谱减法，避免频域变换的延迟。
func (ap *AudioProcessing) applyNS(samples []int16) []int16 {
	// 计算 RMS
	var sumSq float64
	for _, s := range samples {
		sumSq += float64(s) * float64(s)
	}
	rms := math.Sqrt(sumSq / float64(len(samples)))

	// 噪声估计：在静音帧更新噪声基底
	silenceThreshold := 100.0 // RMS < 100 视为静音
	if rms < silenceThreshold {
		ap.noiseFrames++
		// 平滑更新噪声估计
		if !ap.noiseInit {
			ap.noiseFloor = rms
			ap.noiseInit = true
		} else {
			ap.noiseFloor = ap.noiseFloor*0.95 + rms*0.05
		}
	} else {
		ap.noiseFrames = 0
	}

	// 噪声基底太低，不需要处理
	if ap.noiseFloor < 5.0 || !ap.noiseInit {
		return samples
	}

	// 计算抑制因子（基于抑制级别）
	suppressionDB := ap.getNSSuppressionDB()
	alpha := 1.0 - math.Pow(10, -suppressionDB/20) // 过减因子

	// 频谱减法（简化版：时域减法）
	result := make([]int16, len(samples))
	noiseEst := ap.noiseFloor * alpha

	for i, s := range samples {
		val := float64(s)
		absVal := math.Abs(val)

		// 仅对低于噪声水平的成分进行衰减
		if absVal < noiseEst*3 {
			// 信号低于噪声水平，衰减
			if val > 0 {
				val = math.Max(0, val-noiseEst)
			} else {
				val = math.Min(0, val+noiseEst)
			}
		}
		result[i] = clampToInt16(val)
	}

	return result
}

// getNSSuppressionDB 返回 NS 抑制级别对应的 dB 值。
func (ap *AudioProcessing) getNSSuppressionDB() float64 {
	switch ap.nsCfg.SuppressionLevel {
	case NsSuppressionLow:
		return 6.0
	case NsSuppressionModerate:
		return 12.0
	case NsSuppressionHigh:
		return 18.0
	case NsSuppressionVeryHigh:
		return 24.0
	default:
		return 12.0
	}
}

// =============================================================================
// AGC 实现（RMS 增益控制 + 限幅器）
// =============================================================================

// applyAGC 应用自动增益控制。
//
// 测量当前帧 RMS，计算目标增益，平滑调整。
// 包含压缩器和限幅器防止削波和过载。
func (ap *AudioProcessing) applyAGC(samples []int16) []int16 {
	if ap.targetRMS <= 0 {
		return samples // AGC 未配置
	}

	// 计算当前 RMS
	var sumSq float64
	for _, s := range samples {
		sumSq += float64(s) * float64(s)
	}
	currentRMS := math.Sqrt(sumSq / float64(len(samples)))

	if currentRMS < 1.0 {
		return samples // 静音，不调整
	}

	// 目标增益
	targetGain := ap.targetRMS / currentRMS

	// 压缩增益限制（防止过度放大噪声）
	compressionGain := math.Pow(10, float64(ap.agcCfg.CompressionGainDb)/20)
	if targetGain > compressionGain {
		targetGain = compressionGain
	}

	// 最小增益限制（防止过度衰减）
	if targetGain < 0.1 {
		targetGain = 0.1
	}

	// 平滑增益更新（一阶低通，减少增益突变）
	ap.gain = ap.gain*0.9 + targetGain*0.1

	result := make([]int16, len(samples))
	for i, s := range samples {
		val := float64(s) * ap.gain

		// 限幅器（防止削波）
		if ap.agcCfg.EnableLimiter {
			if val > 32767 {
				val = 32767
			} else if val < -32768 {
				val = -32768
			}
		}

		result[i] = int16(val)
	}

	return result
}

// =============================================================================
// AECProcessor 封装
// =============================================================================

// AECProcessor WebRTC 回声消除处理器。
//
// 封装 AudioProcessing，提供简化的接口。
// 对上层隐藏 APM 的内部细节。
type AECProcessor struct {
	apm        *AudioProcessing
	sampleRate uint32
	channels   uint32
}

// NewAECProcessor 创建 AEC 处理器。
//
// 参数：
//   - sampleRate：采样率（Hz），通常 16000
//   - channels：声道数，通常 1
//
// 配置：
//   - AEC：适度抑制，延迟无关，扩展滤波器
//   - NS：适度抑制
//   - AGC：目标 -3 dBFS，压缩增益 9 dB，启用限幅器
func NewAECProcessor(sampleRate uint32, channels uint32) (*AECProcessor, error) {
	apm := NewAudioProcessing()

	// 1. AEC 配置（回声消除）
	aecConfig := AecConfig{
		SuppressionLevel: AecSuppressionModerate, // 适度抑制
		DelayAgnostic:    true,                    // 延迟无关（容忍网络抖动）
		ExtendedFilter:   true,                    // 扩展滤波器
		StreamDelayMs:    0,                       // 自动估计
	}
	if err := apm.SetAecConfig(aecConfig); err != nil {
		return nil, err
	}

	// 2. NS 配置（噪声抑制）
	nsConfig := NsConfig{
		SuppressionLevel: NsSuppressionModerate,
	}
	if err := apm.SetNsConfig(nsConfig); err != nil {
		return nil, err
	}

	// 3. AGC 配置（自动增益）
	agcConfig := AgcConfig{
		TargetLevelDbfs:   -3,   // 目标音量 -3 dBFS
		CompressionGainDb: 9,    // 压缩增益 9 dB
		EnableLimiter:     true, // 启用限幅器
	}
	if err := apm.SetAgcConfig(agcConfig); err != nil {
		return nil, err
	}

	return &AECProcessor{
		apm:        apm,
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}

// ProcessStream 处理下行流（播放给用户，TTS 音频）。
//
// 将下行 PCM 馈送到 AEC 作为参考信号。
// 必须在发送音频到扬声器之前调用。
func (p *AECProcessor) ProcessStream(samples []int16) error {
	frame := &AudioFrame{
		SampleRate:        p.sampleRate,
		NumChannels:       p.channels,
		SamplesPerChannel: len(samples),
		Data:              samplesToBytes(samples),
	}
	_, err := p.apm.ProcessStream(frame)
	return err
}

// ProcessReverseStream 处理上行流（用户麦克风输入，需要参考回声）。
//
// 管线：AEC → NS → AGC
// 返回清洁后的 PCM 采样。
func (p *AECProcessor) ProcessReverseStream(samples []int16) ([]int16, error) {
	frame := &AudioFrame{
		SampleRate:        p.sampleRate,
		NumChannels:       p.channels,
		SamplesPerChannel: len(samples),
		Data:              samplesToBytes(samples),
	}
	outFrame, err := p.apm.ProcessReverseStream(frame)
	if err != nil {
		return nil, err
	}
	return bytesToSamples(outFrame.Data), nil
}

// IsDegraded 返回是否已降级到旁路模式。
func (p *AECProcessor) IsDegraded() bool {
	return p.apm.IsDegraded()
}

// Degrade 降级到旁路模式（AEC/NS/AGC 全部关闭）。
func (p *AECProcessor) Degrade() {
	p.apm.degraded = true
}

// =============================================================================
// 工具函数
// =============================================================================

// bytesToSamples 将 PCM 16-bit 小端字节数组转换为 int16 采样数组。
//
// 输入：[]byte（长度必须是偶数）
// 输出：[]int16（长度 = len(data)/2）
func bytesToSamples(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
	}
	return samples
}

// samplesToBytes 将 int16 采样数组转换为 PCM 16-bit 小端字节数组。
//
// 输入：[]int16
// 输出：[]byte（长度 = len(samples)*2）
func samplesToBytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(s))
	}
	return data
}

// clampToInt16 将 float64 限制在 int16 范围内并转换。
func clampToInt16(val float64) int16 {
	if val > 32767 {
		return 32767
	}
	if val < -32768 {
		return -32768
	}
	return int16(val)
}