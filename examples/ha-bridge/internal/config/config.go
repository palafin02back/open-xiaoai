// Package config 提供配置加载和管理功能
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是应用程序的主配置结构
type Config struct {
	Server        ServerConfig  `yaml:"server"`
	HomeAssistant HAConfig      `yaml:"homeassistant"`
	Audio         AudioConfig   `yaml:"audio"`
	Session       SessionConfig `yaml:"session"`
	Log           LogConfig     `yaml:"log"`
}

// ServerConfig WebSocket 服务器配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// HAConfig Home Assistant 配置
type HAConfig struct {
	URL        string `yaml:"url"`   // Home Assistant URL, 如 http://192.168.1.100:8123
	Token      string `yaml:"token"` // Long-Lived Access Token
	PipelineID string `yaml:"pipeline_id,omitempty"`
}

// AudioConfig 音频配置
type AudioConfig struct {
	InputSampleRate  int     `yaml:"input_sample_rate"`  // 输入采样率，默认 16000
	OutputSampleRate int     `yaml:"output_sample_rate"` // 输出采样率，默认 24000
	Channels         int     `yaml:"channels"`           // 声道数，默认 1
	BitsPerSample    int     `yaml:"bits_per_sample"`    // 位深，默认 16
	VADEnabled       bool    `yaml:"vad_enabled"`        // 是否启用 VAD
	VADModelPath     string  `yaml:"vad_model_path"`     // Silero VAD 模型路径
	VADThreshold     float32 `yaml:"vad_threshold"`      // 高阈值 - 确认有声音 (0-1)
	VADThresholdLow  float32 `yaml:"vad_threshold_low"`  // 低阈值 - 确认无声音 (0-1)
	SilenceTimeout   int     `yaml:"silence_timeout"`    // 静音超时（毫秒）
}

// SessionConfig 会话配置
type SessionConfig struct {
	MultiTurnEnabled   bool          `yaml:"multi_turn_enabled"`    // 多轮对话开关
	WaitSpeechTimeout  time.Duration `yaml:"wait_speech_timeout"`   // 等待用户说话超时
	PostResponseWait   time.Duration `yaml:"post_response_wait"`    // 响应后等待继续
	MaxSessionDuration time.Duration `yaml:"max_session_duration"`  // 最大会话时长
	Prompts            PromptsConfig `yaml:"prompts"`               // 提示语配置
	EndKeywords        []string      `yaml:"end_keywords"`          // 结束关键词
}

// PromptsConfig 提示语配置
type PromptsConfig struct {
	Wakeup   string `yaml:"wakeup"`   // 唤醒提示，如 "在呢"
	Timeout  string `yaml:"timeout"`  // 超时提示
	Goodbye  string `yaml:"goodbye"`  // 主动结束提示
	Continue string `yaml:"continue"` // 继续监听提示（可选）
}

// LogConfig 日志配置
type LogConfig struct {
	Level  string `yaml:"level"`  // 日志级别: debug, info, warn, error
	Format string `yaml:"format"` // 日志格式: json, text
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 4399,
		},
		HomeAssistant: HAConfig{
			URL: "http://localhost:8123",
		},
		Audio: AudioConfig{
			InputSampleRate:  16000,
			OutputSampleRate: 24000,
			Channels:         1,
			BitsPerSample:    16,
			VADEnabled:       true,
			VADModelPath:     "models/silero_vad.onnx",
			VADThreshold:     0.5,
			VADThresholdLow:  0.2,
			SilenceTimeout:   1200,
		},
		Session: SessionConfig{
			MultiTurnEnabled:   true,
			WaitSpeechTimeout:  8 * time.Second,
			PostResponseWait:   4 * time.Second,
			MaxSessionDuration: 3 * time.Minute,
			Prompts: PromptsConfig{
				Wakeup:  "在呢",
				Timeout: "下次需要帮忙再叫我哦~",
				Goodbye: "好的，再见~",
			},
			EndKeywords: []string{
				"没有了", "没了", "谢谢", "再见",
				"退出", "结束", "好了", "不用了",
			},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}
