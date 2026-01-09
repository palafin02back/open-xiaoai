// Package xiaoai 提供小爱音箱客户端连接管理
package xiaoai

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Client 表示一个连接的小爱音箱客户端
type Client struct {
	ID         string
	conn       *websocket.Conn
	logger     *zap.Logger
	writeMu    sync.Mutex
	closeChan  chan struct{}
	closeOnce  sync.Once
	lastActive time.Time

	// 待处理响应回调
	pendingMu    sync.RWMutex
	pendingCalls map[string]chan *Response
}

// NewClient 创建新的客户端实例
func NewClient(conn *websocket.Conn, logger *zap.Logger) *Client {
	return &Client{
		ID:           uuid.New().String(),
		conn:         conn,
		logger:       logger,
		closeChan:    make(chan struct{}),
		lastActive:   time.Now(),
		pendingCalls: make(map[string]chan *Response),
	}
}

// Run 运行客户端消息处理循环
func (c *Client) Run(handler func(*Client, int, []byte)) {
	for {
		select {
		case <-c.closeChan:
			return
		default:
			msgType, data, err := c.conn.ReadMessage()
			if err != nil {
				c.logger.Debug("Read error", zap.Error(err))
				return
			}
			c.lastActive = time.Now()
			handler(c, msgType, data)
		}
	}
}

// Close 关闭客户端连接
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.closeChan)
		c.conn.Close()
		
		// 取消所有待处理的调用
		c.pendingMu.Lock()
		for _, ch := range c.pendingCalls {
			close(ch)
		}
		c.pendingCalls = make(map[string]chan *Response)
		c.pendingMu.Unlock()
	})
}

// SendResponse 发送 Response 消息
func (c *Client) SendResponse(id string, code int, msg string, data json.RawMessage) error {
	resp := &AppMessage{
		Response: &Response{
			ID:   id,
			Code: &code,
			Msg:  &msg,
			Data: data,
		},
	}
	return c.sendJSON(resp)
}

// SendRequest 发送 Request 消息（RPC 调用）
func (c *Client) SendRequest(command string, payload interface{}) (string, error) {
	id := uuid.New().String()

	var payloadJSON json.RawMessage
	if payload != nil {
		var err error
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return "", err
		}
	}

	req := &AppMessage{
		Request: &Request{
			ID:      id,
			Command: command,
			Payload: payloadJSON,
		},
	}

	return id, c.sendJSON(req)
}

// SendRequestAndWait 发送请求并等待响应
func (c *Client) SendRequestAndWait(command string, payload interface{}, timeout time.Duration) (*Response, error) {
	id := uuid.New().String()

	// 创建响应通道
	respChan := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pendingCalls[id] = respChan
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pendingCalls, id)
		c.pendingMu.Unlock()
	}()

	// 发送请求
	var payloadJSON json.RawMessage
	if payload != nil {
		var err error
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	req := &AppMessage{
		Request: &Request{
			ID:      id,
			Command: command,
			Payload: payloadJSON,
		},
	}

	if err := c.sendJSON(req); err != nil {
		return nil, err
	}

	// 等待响应
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-respChan:
		return resp, nil
	case <-timer.C:
		return nil, ErrTimeout
	case <-c.closeChan:
		return nil, ErrClosed
	}
}

// HandleResponse 处理收到的响应（由 Server 调用）
func (c *Client) HandleResponse(resp *Response) {
	c.pendingMu.RLock()
	ch, ok := c.pendingCalls[resp.ID]
	c.pendingMu.RUnlock()

	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

// SendStream 发送音频流
func (c *Client) SendStream(tag string, bytes []byte) error {
	stream := &Stream{
		ID:    uuid.New().String(),
		Tag:   tag,
		Bytes: bytes,
	}

	data, err := json.Marshal(stream)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

// StartRecording 启动录音
func (c *Client) StartRecording(config *AudioConfigPayload) error {
	_, err := c.SendRequest(CmdStartRecording, config)
	return err
}

// StopRecording 停止录音
func (c *Client) StopRecording() error {
	_, err := c.SendRequest(CmdStopRecording, nil)
	return err
}

// StartPlay 启动播放
func (c *Client) StartPlay(config *AudioConfigPayload) error {
	_, err := c.SendRequest(CmdStartPlay, config)
	return err
}

// StopPlay 停止播放
func (c *Client) StopPlay() error {
	_, err := c.SendRequest(CmdStopPlay, nil)
	return err
}

// RunShell 在小爱音箱上执行 Shell 命令
func (c *Client) RunShell(script string) error {
	_, err := c.SendRequest(CmdRunShell, script)
	return err
}

// AbortXiaoAI 中断小爱原生处理
func (c *Client) AbortXiaoAI() error {
	return c.RunShell("/etc/init.d/mico_aivs_lab restart")
}

// PlayText 播放 TTS 文字
func (c *Client) PlayText(text string) error {
	// 转义单引号，防止命令注入
	escaped := strings.ReplaceAll(text, "'", "'\\''")
	script := "/usr/sbin/tts_play.sh '" + escaped + "'"
	return c.RunShell(script)
}

// LastActive 返回最后活跃时间
func (c *Client) LastActive() time.Time {
	return c.lastActive
}

func (c *Client) sendJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

// 错误定义
var (
	ErrTimeout = &ClientError{msg: "request timeout"}
	ErrClosed  = &ClientError{msg: "client closed"}
)

// ClientError 客户端错误
type ClientError struct {
	msg string
}

func (e *ClientError) Error() string {
	return e.msg
}
