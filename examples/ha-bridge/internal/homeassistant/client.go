// Package homeassistant 提供 HA WebSocket 客户端
package homeassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/config"
)

// Client Home Assistant WebSocket 客户端
type Client struct {
	config *config.HAConfig
	logger *zap.Logger
	conn   *websocket.Conn
	msgID  int
	msgMu  sync.Mutex

	// 认证状态
	authenticated bool
}

// NewClient 创建新的 HA 客户端
func NewClient(cfg *config.HAConfig, logger *zap.Logger) *Client {
	return &Client{
		config: cfg,
		logger: logger,
	}
}

// Connect 连接并认证
func (c *Client) Connect(ctx context.Context) error {
	// 构建 WebSocket URL
	wsURL := c.buildWebSocketURL()
	c.logger.Debug("Connecting to Home Assistant", zap.String("url", wsURL))

	// 连接
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	c.conn = conn

	// 等待 auth_required
	var authReq struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&authReq); err != nil {
		conn.Close()
		return fmt.Errorf("read auth_required failed: %w", err)
	}

	if authReq.Type != "auth_required" {
		conn.Close()
		return fmt.Errorf("expected auth_required, got %s", authReq.Type)
	}

	// 发送认证
	auth := map[string]string{
		"type":         "auth",
		"access_token": c.config.Token,
	}
	if err := conn.WriteJSON(auth); err != nil {
		conn.Close()
		return fmt.Errorf("send auth failed: %w", err)
	}

	// 等待 auth_ok 或 auth_invalid
	var authResp struct {
		Type    string `json:"type"`
		Message string `json:"message,omitempty"`
	}
	if err := conn.ReadJSON(&authResp); err != nil {
		conn.Close()
		return fmt.Errorf("read auth response failed: %w", err)
	}

	if authResp.Type == "auth_invalid" {
		conn.Close()
		return fmt.Errorf("authentication failed: %s", authResp.Message)
	}

	if authResp.Type != "auth_ok" {
		conn.Close()
		return fmt.Errorf("unexpected auth response: %s", authResp.Type)
	}

	c.authenticated = true
	c.logger.Info("Connected to Home Assistant")
	return nil
}

// Close 关闭连接
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.authenticated = false
}

// IsConnected 是否已连接
func (c *Client) IsConnected() bool {
	return c.conn != nil && c.authenticated
}

// NextMsgID 获取下一个消息 ID
func (c *Client) NextMsgID() int {
	c.msgMu.Lock()
	defer c.msgMu.Unlock()
	c.msgID++
	return c.msgID
}

// WriteJSON 发送 JSON 消息
func (c *Client) WriteJSON(v interface{}) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.WriteJSON(v)
}

// ReadJSON 读取 JSON 消息
func (c *Client) ReadJSON(v interface{}) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.ReadJSON(v)
}

// WriteMessage 发送消息
func (c *Client) WriteMessage(messageType int, data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.WriteMessage(messageType, data)
}

// ReadMessage 读取消息
func (c *Client) ReadMessage() (int, []byte, error) {
	if c.conn == nil {
		return 0, nil, fmt.Errorf("not connected")
	}
	return c.conn.ReadMessage()
}

// HTTPGet 发送 HTTP GET 请求
func (c *Client) HTTPGet(path string) ([]byte, error) {
	url := c.config.URL + path

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.config.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// HTTPPost 发送 HTTP POST 请求
func (c *Client) HTTPPost(path string, body interface{}) ([]byte, error) {
	url := c.config.URL + path

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, io.NopCloser(bytes.NewReader(bodyJSON)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (c *Client) buildWebSocketURL() string {
	// http://xxx -> ws://xxx
	// https://xxx -> wss://xxx
	url := c.config.URL
	if len(url) >= 5 && url[:5] == "https" {
		return "wss" + url[5:] + "/api/websocket"
	}
	if len(url) >= 4 && url[:4] == "http" {
		return "ws" + url[4:] + "/api/websocket"
	}
	return "ws://" + url + "/api/websocket"
}

// GetConfig 获取配置
func (c *Client) GetConfig() *config.HAConfig {
	return c.config
}
