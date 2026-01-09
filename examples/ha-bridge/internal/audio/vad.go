// Package audio 提供 VAD（语音活动检测）功能
// 使用 Silero VAD (ONNX) 实现高精度语音检测
// 参考 xiaozhi-esp32-server 的设计：双阈值、滑动窗口、时间戳计算
package audio

import (
	"fmt"
	"sync"
	"time"

	speech "github.com/streamer45/silero-vad-go/speech"
)

// VADResult VAD 检测结果
type VADResult int

const (
	// VADResultSilence 静音
	VADResultSilence VADResult = iota
	// VADResultSpeech 检测到语音
	VADResultSpeech
	// VADResultSpeechEnd 语音结束
	VADResultSpeechEnd
)

// String 返回结果的字符串表示
func (r VADResult) String() string {
	switch r {
	case VADResultSilence:
		return "silence"
	case VADResultSpeech:
		return "speech"
	case VADResultSpeechEnd:
		return "speech_end"
	default:
		return "unknown"
	}
}

// VADConfig VAD 配置
type VADConfig struct {
	ModelPath          string  // silero_vad.onnx 模型路径
	SampleRate         int     // 采样率，必须为 8000 或 16000
	SpeechThreshold    float32 // 高阈值 - 超过此值确认有声音 (推荐 0.5)
	SpeechThresholdLow float32 // 低阈值 - 低于此值确认无声音 (推荐 0.2)
	SilenceTimeoutMs   int     // 静音超时（毫秒）
	MinSpeechMs        int     // 最小语音时长（毫秒）
	WindowSize         int     // 滑动窗口大小（帧数，推荐 5）
	WindowThreshold    int     // 滑动窗口中需要多少帧才算有语音（推荐 3）
}

// DefaultVADConfig 返回默认 VAD 配置
func DefaultVADConfig() VADConfig {
	return VADConfig{
		ModelPath:          "models/silero_vad.onnx",
		SampleRate:         16000,
		SpeechThreshold:    0.5,
		SpeechThresholdLow: 0.2,
		SilenceTimeoutMs:   1200,
		MinSpeechMs:        100,
		WindowSize:         5,
		WindowThreshold:    3,
	}
}

// VAD 语音活动检测器（基于 Silero VAD）
type VAD struct {
	detector *speech.Detector
	config   VADConfig
	mu       sync.Mutex

	// 双阈值状态
	lastIsVoice bool // 上一帧的语音状态

	// 滑动窗口
	voiceWindow []bool // 语音检测滑动窗口

	// 语音状态
	isSpeaking       bool      // 是否正在说话
	haveVoice        bool      // 滑动窗口确认有语音
	lastActivityTime time.Time // 最后活动时间

	// 音频缓冲（Silero 需要 512 样本窗口）
	audioBuffer    []float32
	sileroWinSize  int // 512 for 16kHz, 256 for 8kHz
	minSpeechMs    int
	minSpeechStart time.Time // 语音开始时间
}

// NewVAD 创建新的 VAD 实例
func NewVAD(cfg VADConfig) (*VAD, error) {
	// 验证参数
	if cfg.SampleRate != 8000 && cfg.SampleRate != 16000 {
		return nil, fmt.Errorf("sample rate must be 8000 or 16000, got %d", cfg.SampleRate)
	}

	if cfg.SpeechThreshold <= 0 || cfg.SpeechThreshold >= 1 {
		return nil, fmt.Errorf("speech threshold must be between 0 and 1, got %f", cfg.SpeechThreshold)
	}

	if cfg.SpeechThresholdLow <= 0 || cfg.SpeechThresholdLow >= cfg.SpeechThreshold {
		cfg.SpeechThresholdLow = cfg.SpeechThreshold * 0.4 // 默认为高阈值的 40%
	}

	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 5
	}
	if cfg.WindowThreshold <= 0 || cfg.WindowThreshold > cfg.WindowSize {
		cfg.WindowThreshold = 3
	}

	// 创建 Silero VAD 检测器
	detector, err := speech.NewDetector(speech.DetectorConfig{
		ModelPath:            cfg.ModelPath,
		SampleRate:           cfg.SampleRate,
		Threshold:            cfg.SpeechThreshold,
		MinSilenceDurationMs: cfg.SilenceTimeoutMs,
		SpeechPadMs:          30,
	})
	if err != nil {
		return nil, fmt.Errorf("create silero detector failed: %w", err)
	}

	// Silero 需要的窗口大小
	sileroWinSize := 512
	if cfg.SampleRate == 8000 {
		sileroWinSize = 256
	}

	return &VAD{
		detector:      detector,
		config:        cfg,
		voiceWindow:   make([]bool, 0, cfg.WindowSize),
		sileroWinSize: sileroWinSize,
		audioBuffer:   make([]float32, 0, sileroWinSize*4),
		minSpeechMs:   cfg.MinSpeechMs,
	}, nil
}

// ExpectedFrameSize 返回期望的帧大小（字节）
// 30ms @ 16kHz = 480 samples = 960 bytes
func (v *VAD) ExpectedFrameSize() int {
	samples := v.config.SampleRate * 30 / 1000 // 30ms
	return samples * 2                          // 16-bit = 2 bytes per sample
}

// Process 处理一帧音频数据
// frame: 16-bit PCM 音频数据
// 返回 VAD 检测结果
func (v *VAD) Process(frame []byte) VADResult {
	v.mu.Lock()
	defer v.mu.Unlock()

	// 转换为 float32 样本
	samples := bytesToFloat32(frame)

	// 添加到缓冲区
	v.audioBuffer = append(v.audioBuffer, samples...)

	// 如果缓冲区不够一个窗口，返回当前状态
	if len(v.audioBuffer) < v.sileroWinSize {
		if v.isSpeaking {
			return VADResultSpeech
		}
		return VADResultSilence
	}

	// 处理完整的窗口
	var speechProb float32 = 0
	windowProcessed := false

	for len(v.audioBuffer) >= v.sileroWinSize {
		// 提取一个窗口
		window := v.audioBuffer[:v.sileroWinSize]
		v.audioBuffer = v.audioBuffer[v.sileroWinSize:]

		// Silero VAD 检测
		segments, err := v.detector.Detect(window)
		if err == nil && len(segments) > 0 {
			speechProb = 1.0 // 检测到语音
		}
		windowProcessed = true
	}

	if !windowProcessed {
		if v.isSpeaking {
			return VADResultSpeech
		}
		return VADResultSilence
	}

	// === 双阈值判断 ===
	var isVoice bool
	if speechProb >= v.config.SpeechThreshold {
		// 高于高阈值，确认有声音
		isVoice = true
	} else if speechProb <= v.config.SpeechThresholdLow {
		// 低于低阈值，确认无声音
		isVoice = false
	} else {
		// 在两个阈值之间，延续上一个状态
		isVoice = v.lastIsVoice
	}
	v.lastIsVoice = isVoice

	// === 滑动窗口更新 ===
	v.voiceWindow = append(v.voiceWindow, isVoice)
	if len(v.voiceWindow) > v.config.WindowSize {
		v.voiceWindow = v.voiceWindow[1:]
	}

	// 统计窗口中有语音的帧数
	trueCount := 0
	for _, val := range v.voiceWindow {
		if val {
			trueCount++
		}
	}
	windowHaveVoice := trueCount >= v.config.WindowThreshold

	// === 状态机判断 ===
	now := time.Now()

	// 如果之前有声音，但本次滑动窗口没有声音
	if v.haveVoice && !windowHaveVoice {
		// 检查静音时长
		silenceDuration := now.Sub(v.lastActivityTime)
		if silenceDuration.Milliseconds() >= int64(v.config.SilenceTimeoutMs) {
			// 语音结束
			v.isSpeaking = false
			v.haveVoice = false
			return VADResultSpeechEnd
		}
	}

	// 如果滑动窗口确认有语音
	if windowHaveVoice {
		if !v.haveVoice {
			// 语音刚开始
			v.minSpeechStart = now
		}
		v.haveVoice = true
		v.lastActivityTime = now

		// 检查是否达到最小语音时长
		if !v.isSpeaking {
			speechDuration := now.Sub(v.minSpeechStart)
			if speechDuration.Milliseconds() >= int64(v.minSpeechMs) {
				v.isSpeaking = true
			}
		}
	}

	if v.isSpeaking {
		return VADResultSpeech
	}
	return VADResultSilence
}

// bytesToFloat32 将 16-bit Little-Endian PCM 转换为 float32 [-1, 1]
func bytesToFloat32(data []byte) []float32 {
	samples := make([]float32, len(data)/2)
	for i := 0; i < len(data)-1; i += 2 {
		sample := int16(data[i]) | int16(data[i+1])<<8
		samples[i/2] = float32(sample) / 32768.0
	}
	return samples
}

// Reset 重置 VAD 状态
func (v *VAD) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.lastIsVoice = false
	v.voiceWindow = v.voiceWindow[:0]
	v.isSpeaking = false
	v.haveVoice = false
	v.lastActivityTime = time.Time{}
	v.audioBuffer = v.audioBuffer[:0]
	v.minSpeechStart = time.Time{}
	v.detector.Reset()
}

// IsSpeaking 返回当前是否正在说话
func (v *VAD) IsSpeaking() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.isSpeaking
}

// GetConfig 获取当前配置
func (v *VAD) GetConfig() VADConfig {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.config
}

// Close 释放资源
func (v *VAD) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.detector != nil {
		v.detector.Destroy()
		v.detector = nil
	}
}
