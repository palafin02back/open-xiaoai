// Package main 是 Open-XiaoAI HA Bridge 的入口
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/config"
	"github.com/open-xiaoai/ha-bridge/internal/session"
	"github.com/open-xiaoai/ha-bridge/internal/xiaoai"
	"github.com/open-xiaoai/ha-bridge/pkg/logger"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "config.yaml", "config file path")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Open-XiaoAI HA Bridge %s (built: %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	// 初始化日志（临时）
	log, _ := zap.NewDevelopment()
	defer log.Sync()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("Failed to load config", zap.String("path", *configPath), zap.Error(err))
	}

	// 使用配置初始化正式日志
	log, err = logger.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		log.Fatal("Failed to create logger", zap.Error(err))
	}
	defer log.Sync()

	log.Info("Starting Open-XiaoAI HA Bridge",
		zap.String("version", Version),
		zap.String("config", *configPath))

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化会话管理器
	sessionMgr := session.NewManager(&cfg.Session, log)
	go sessionMgr.CleanupLoop(ctx)

	// 创建 Bridge
	bridge := NewBridge(cfg, sessionMgr, log)

	// 创建 XiaoAI WebSocket 服务器
	server := xiaoai.NewServer(&cfg.Server, log)

	// 注册事件处理
	server.OnClientConnect(func(client *xiaoai.Client) {
		log.Info("Client connected",
			zap.String("id", client.ID))
		sessionMgr.GetOrCreateSession(client.ID)
	})

	server.OnClientDisconnect(func(client *xiaoai.Client) {
		log.Info("Client disconnected",
			zap.String("id", client.ID))
		sessionMgr.RemoveSession(client.ID)
		bridge.RemoveVAD(client.ID) // 清理 VAD 实例
	})

	server.OnEvent(func(client *xiaoai.Client, event *xiaoai.Event) {
		bridge.HandleEvent(ctx, client, event)
	})

	server.OnStream(func(client *xiaoai.Client, stream *xiaoai.Stream) {
		bridge.HandleStream(ctx, client, stream)
	})

	// 启动服务器
	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		log.Info("WebSocket server starting", zap.String("addr", addr))

		if err := server.Start(ctx); err != nil {
			log.Error("Server error", zap.Error(err))
		}
	}()

	log.Info("Bridge started",
		zap.String("listen", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)),
		zap.String("ha_url", cfg.HomeAssistant.URL))

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	log.Info("Received signal, shutting down...", zap.String("signal", sig.String()))
	cancel()

	log.Info("Goodbye!")
}
