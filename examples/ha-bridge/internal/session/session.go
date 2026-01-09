// Package session 提供单个会话管理
package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/config"
)

// Session 表示一个语音交互会话
type Session struct {
	ID             string
	ClientID       string
	ConversationID string // HA 会话 ID，用于多轮对话上下文

	state          State
	stateMu        sync.RWMutex

	StartTime      time.Time
	LastActiveTime time.Time
	RoundCount     int // 对话轮数

	// 音频通道
	audioChan chan []byte
	
	// 语音检测信号
	speechDetected chan struct{}
	
	// 音频结束信号（通知 Pipeline 停止接收）
	audioEndChan chan struct{}

	// 会话控制
	ctx        context.Context
	cancelFunc context.CancelFunc
	
	// 关闭控制
	closed    bool
	closeMu   sync.Mutex

	config *config.SessionConfig
	logger *zap.Logger
}

// NewSession 创建新会话
func NewSession(id, clientID string, cfg *config.SessionConfig, logger *zap.Logger) *Session {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.MaxSessionDuration)
	
	return &Session{
		ID:             id,
		ClientID:       clientID,
		state:          StateIdle,
		StartTime:      time.Now(),
		LastActiveTime: time.Now(),
		audioChan:      make(chan []byte, 100),
		speechDetected: make(chan struct{}, 1),
		audioEndChan:   make(chan struct{}),
		ctx:            ctx,
		cancelFunc:     cancel,
		config:         cfg,
		logger:         logger.With(zap.String("session", id)),
	}
}

// GetState 获取当前状态
func (s *Session) GetState() State {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

// SetState 设置状态
func (s *Session) SetState(state State) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	
	s.logger.Debug("State changed",
		zap.String("from", s.state.String()),
		zap.String("to", state.String()))
	s.state = state
	s.LastActiveTime = time.Now()
}

// Touch 更新最后活跃时间
func (s *Session) Touch() {
	s.stateMu.Lock()
	s.LastActiveTime = time.Now()
	s.stateMu.Unlock()
}

// PushAudio 推送音频数据到会话
func (s *Session) PushAudio(data []byte) {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return
	}
	s.closeMu.Unlock()
	
	select {
	case s.audioChan <- data:
	default:
		s.logger.Warn("Audio buffer full, dropping")
	}
}

// AudioStream 获取音频流通道（只读）
func (s *Session) AudioStream() <-chan []byte {
	return s.audioChan
}

// DrainAudio 清空音频缓冲
func (s *Session) DrainAudio() {
	for {
		select {
		case <-s.audioChan:
		default:
			return
		}
	}
}

// NotifySpeechDetected 通知检测到语音
func (s *Session) NotifySpeechDetected() {
	select {
	case s.speechDetected <- struct{}{}:
	default:
	}
}

// WaitForSpeech 等待用户开始说话
func (s *Session) WaitForSpeech(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-s.ctx.Done():
		return false
	case <-timer.C:
		return false
	case <-s.speechDetected:
		return true
	}
}

// IsEndIntent 检查是否为结束意图
func (s *Session) IsEndIntent(text string) bool {
	for _, keyword := range s.config.EndKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// IncrementRound 增加对话轮数
func (s *Session) IncrementRound() {
	s.RoundCount++
	s.LastActiveTime = time.Now()
}

// Context 返回会话上下文
func (s *Session) Context() context.Context {
	return s.ctx
}

// Cancel 取消会话
func (s *Session) Cancel() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

// SignalAudioEnd 通知音频流结束
func (s *Session) SignalAudioEnd() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	
	select {
	case <-s.audioEndChan:
		// 已经关闭
	default:
		close(s.audioEndChan)
	}
}

// AudioEndChan 返回音频结束通道
func (s *Session) AudioEndChan() <-chan struct{} {
	return s.audioEndChan
}

// Close 关闭会话
func (s *Session) Close() {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return
	}
	s.closed = true
	s.closeMu.Unlock()
	
	s.Cancel()
	
	// 关闭音频结束通道
	s.SignalAudioEnd()
	
	// 关闭音频通道
	close(s.audioChan)
	
	s.logger.Info("Session closed",
		zap.Int("rounds", s.RoundCount),
		zap.Duration("duration", time.Since(s.StartTime)))
}

// IsClosed 检查会话是否已关闭
func (s *Session) IsClosed() bool {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	return s.closed
}

// Config 返回会话配置
func (s *Session) Config() *config.SessionConfig {
	return s.config
}

// IsMultiTurnEnabled 是否启用多轮对话
func (s *Session) IsMultiTurnEnabled() bool {
	return s.config.MultiTurnEnabled
}
