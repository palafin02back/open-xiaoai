// Package session 提供会话管理器
package session

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/config"
)

// Manager 会话管理器
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	config   *config.SessionConfig
	logger   *zap.Logger
}

// NewManager 创建会话管理器
func NewManager(cfg *config.SessionConfig, logger *zap.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		config:   cfg,
		logger:   logger,
	}
}

// GetOrCreateSession 获取或创建会话
func (m *Manager) GetOrCreateSession(clientID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, exists := m.sessions[clientID]; exists {
		s.Touch()
		return s
	}

	sessionID := uuid.New().String()
	s := NewSession(sessionID, clientID, m.config, m.logger)
	m.sessions[clientID] = s

	m.logger.Info("Session created",
		zap.String("session_id", sessionID),
		zap.String("client_id", clientID))

	return s
}

// GetSession 获取指定客户端的会话
func (m *Manager) GetSession(clientID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[clientID]
}

// RemoveSession 移除会话
func (m *Manager) RemoveSession(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, exists := m.sessions[clientID]; exists {
		s.Close()
		delete(m.sessions, clientID)
		m.logger.Info("Session removed", zap.String("client_id", clientID))
	}
}

// ResetSession 重置会话（用于新一轮交互）
func (m *Manager) ResetSession(clientID string) *Session {
	m.RemoveSession(clientID)
	return m.GetOrCreateSession(clientID)
}

// CleanupLoop 定期清理过期会话
func (m *Manager) CleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	for clientID, s := range m.sessions {
		// 检查会话是否超时
		if s.GetState() == StateIdle {
			idleTime := now.Sub(s.LastActiveTime)
			if idleTime > 5*time.Minute {
				s.Close()
				delete(m.sessions, clientID)
				m.logger.Info("Session expired",
					zap.String("client_id", clientID),
					zap.Duration("idle_time", idleTime))
			}
		}
	}
}

// Count 返回当前会话数量
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// GetAllSessions 获取所有会话
func (m *Manager) GetAllSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}
