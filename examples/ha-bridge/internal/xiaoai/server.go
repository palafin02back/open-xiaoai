// Package xiaoai 提供 WebSocket 服务器
package xiaoai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/config"
)

// Server WebSocket 服务器
type Server struct {
	config   *config.ServerConfig
	upgrader websocket.Upgrader
	clients  map[string]*Client
	mu       sync.RWMutex
	logger   *zap.Logger

	// 事件回调
	onClientConnect    func(client *Client)
	onClientDisconnect func(client *Client)
	onEvent            func(client *Client, event *Event)
	onStream           func(client *Client, stream *Stream)
	onRequest          func(client *Client, request *Request)
}

// NewServer 创建新的 WebSocket 服务器
func NewServer(cfg *config.ServerConfig, logger *zap.Logger) *Server {
	return &Server{
		config: cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients: make(map[string]*Client),
		logger:  logger,
	}
}

// Start 启动服务器
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.logger.Info("Starting XiaoAI WebSocket server", zap.String("addr", addr))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		s.logger.Info("Shutting down WebSocket server")
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("WebSocket upgrade failed", zap.Error(err))
		return
	}

	client := NewClient(conn, s.logger)
	s.registerClient(client)

	defer func() {
		s.unregisterClient(client)
		client.Close()
	}()

	s.logger.Info("Client connected",
		zap.String("id", client.ID),
		zap.String("remote", r.RemoteAddr))

	if s.onClientConnect != nil {
		s.onClientConnect(client)
	}

	client.Run(s.handleMessage)

	s.logger.Info("Client disconnected", zap.String("id", client.ID))
	if s.onClientDisconnect != nil {
		s.onClientDisconnect(client)
	}
}

func (s *Server) handleMessage(client *Client, msgType int, data []byte) {
	if msgType == websocket.BinaryMessage {
		// 处理二进制消息（Stream）
		var stream Stream
		if err := json.Unmarshal(data, &stream); err != nil {
			s.logger.Error("Failed to parse stream", zap.Error(err))
			return
		}
		if s.onStream != nil {
			s.onStream(client, &stream)
		}
		return
	}

	// 处理文本消息
	var msg AppMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		s.logger.Error("Failed to parse message", zap.Error(err), zap.String("data", string(data)))
		return
	}

	if msg.Request != nil {
		s.handleRequest(client, msg.Request)
	} else if msg.Response != nil {
		// 处理响应（用于 RPC 回调）
		client.HandleResponse(msg.Response)
	} else if msg.Event != nil {
		s.logger.Debug("Received event",
			zap.String("event", msg.Event.Event),
			zap.String("client", client.ID))
		if s.onEvent != nil {
			s.onEvent(client, msg.Event)
		}
	} else if msg.Stream != nil {
		if s.onStream != nil {
			s.onStream(client, msg.Stream)
		}
	}
}

func (s *Server) handleRequest(client *Client, req *Request) {
	s.logger.Debug("Received request",
		zap.String("command", req.Command),
		zap.String("client", client.ID))

	switch req.Command {
	case CmdGetVersion:
		client.SendResponse(req.ID, 0, "success", json.RawMessage(`"1.0.0"`))
	default:
		// 转发给外部处理器
		if s.onRequest != nil {
			s.onRequest(client, req)
		} else {
			s.logger.Warn("Unknown command", zap.String("command", req.Command))
			client.SendResponse(req.ID, -1, "unknown command", nil)
		}
	}
}

func (s *Server) registerClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[client.ID] = client
}

func (s *Server) unregisterClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, client.ID)
}

// GetClient 获取指定 ID 的客户端
func (s *Server) GetClient(id string) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[id]
}

// GetClients 获取所有客户端
func (s *Server) GetClients() []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	return clients
}

// ClientCount 返回当前连接的客户端数量
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// OnClientConnect 注册客户端连接回调
func (s *Server) OnClientConnect(fn func(*Client)) {
	s.onClientConnect = fn
}

// OnClientDisconnect 注册客户端断开回调
func (s *Server) OnClientDisconnect(fn func(*Client)) {
	s.onClientDisconnect = fn
}

// OnEvent 注册事件回调
func (s *Server) OnEvent(fn func(*Client, *Event)) {
	s.onEvent = fn
}

// OnStream 注册流回调
func (s *Server) OnStream(fn func(*Client, *Stream)) {
	s.onStream = fn
}

// OnRequest 注册请求回调
func (s *Server) OnRequest(fn func(*Client, *Request)) {
	s.onRequest = fn
}
