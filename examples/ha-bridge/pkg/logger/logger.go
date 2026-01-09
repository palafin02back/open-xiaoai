// Package logger 提供日志工具
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New 创建一个新的日志实例
func New(level, format string) (*zap.Logger, error) {
	var cfg zap.Config

	if format == "text" {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		cfg = zap.NewProductionConfig()
	}

	// 设置日志级别
	switch level {
	case "debug":
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	return cfg.Build()
}

// NewDefault 创建默认的日志实例（info级别，json格式）
func NewDefault() *zap.Logger {
	logger, _ := New("info", "json")
	return logger
}
