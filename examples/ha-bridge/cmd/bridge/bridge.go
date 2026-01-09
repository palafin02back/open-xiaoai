// Package main 提供 Bridge 核心逻辑
package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/audio"
	"github.com/open-xiaoai/ha-bridge/internal/config"
	"github.com/open-xiaoai/ha-bridge/internal/homeassistant"
	"github.com/open-xiaoai/ha-bridge/internal/session"
	"github.com/open-xiaoai/ha-bridge/internal/xiaoai"
)

// Bridge 核心桥接逻辑
type Bridge struct {
	config     *config.Config
	sessionMgr *session.Manager
	logger     *zap.Logger

	// VAD 实例（每个客户端一个）
	vadInstances map[string]*audio.VAD
	vadMu        sync.RWMutex
}

// NewBridge 创建 Bridge 实例
func NewBridge(cfg *config.Config, sessionMgr *session.Manager, logger *zap.Logger) *Bridge {
	return &Bridge{
		config:       cfg,
		sessionMgr:   sessionMgr,
		logger:       logger,
		vadInstances: make(map[string]*audio.VAD),
	}
}

// HandleEvent 处理来自小爱音箱的事件
func (b *Bridge) HandleEvent(ctx context.Context, client *xiaoai.Client, event *xiaoai.Event) {
	sess := b.sessionMgr.GetOrCreateSession(client.ID)

	switch event.Event {
	case xiaoai.EventKWS:
		b.handleKWSEvent(ctx, client, sess, event)
	case xiaoai.EventInstruction:
		b.handleInstructionEvent(ctx, client, sess, event)
	case xiaoai.EventPlaying:
		b.handlePlayingEvent(ctx, client, sess, event)
	default:
		b.logger.Debug("Unknown event", zap.String("event", event.Event))
	}
}

// HandleStream 处理来自小爱音箱的音频流
func (b *Bridge) HandleStream(ctx context.Context, client *xiaoai.Client, stream *xiaoai.Stream) {
	sess := b.sessionMgr.GetSession(client.ID)
	if sess == nil {
		return
	}

	if stream.Tag == xiaoai.StreamTagRecord {
		state := sess.GetState()
		if state == session.StateListening || state == session.StateRecording {
			// 获取或创建 VAD 实例
			vad := b.getOrCreateVAD(client.ID)

			// VAD 检测
			result := vad.Process(stream.Bytes)

			switch result {
			case audio.VADResultSpeech:
				// 检测到语音，通知会话
				sess.NotifySpeechDetected()
				sess.PushAudio(stream.Bytes)

			case audio.VADResultSpeechEnd:
				// 语音结束，推送最后一帧并通知 Pipeline 停止
				b.logger.Debug("VAD detected speech end")
				sess.PushAudio(stream.Bytes)
				sess.SignalAudioEnd()
				// 重置 VAD 状态，准备下一轮
				vad.Reset()

			case audio.VADResultSilence:
				// 静音，如果已经开始说话则继续推送
				if vad.IsSpeaking() {
					sess.PushAudio(stream.Bytes)
				}
			}
		}
	}
}

// getOrCreateVAD 获取或创建客户端的 VAD 实例
func (b *Bridge) getOrCreateVAD(clientID string) *audio.VAD {
	b.vadMu.RLock()
	vad, exists := b.vadInstances[clientID]
	b.vadMu.RUnlock()

	if exists {
		return vad
	}

	b.vadMu.Lock()
	defer b.vadMu.Unlock()

	// 双重检查
	if vad, exists = b.vadInstances[clientID]; exists {
		return vad
	}

	// 创建新的 VAD 实例
	vadConfig := audio.VADConfig{
		ModelPath:          b.config.Audio.VADModelPath,
		SampleRate:         b.config.Audio.InputSampleRate,
		SpeechThreshold:    b.config.Audio.VADThreshold,
		SpeechThresholdLow: b.config.Audio.VADThresholdLow,
		SilenceTimeoutMs:   b.config.Audio.SilenceTimeout,
		MinSpeechMs:        100,
		WindowSize:         5,
		WindowThreshold:    3,
	}

	vad, err := audio.NewVAD(vadConfig)
	if err != nil {
		b.logger.Error("Create VAD failed", zap.Error(err))
		// 使用默认配置
		vad, _ = audio.NewVAD(audio.DefaultVADConfig())
	}

	b.vadInstances[clientID] = vad
	return vad
}

// RemoveVAD 移除客户端的 VAD 实例
func (b *Bridge) RemoveVAD(clientID string) {
	b.vadMu.Lock()
	defer b.vadMu.Unlock()
	delete(b.vadInstances, clientID)
}

// handleKWSEvent 处理唤醒词事件（来自自定义KWS）
func (b *Bridge) handleKWSEvent(ctx context.Context, client *xiaoai.Client, sess *session.Session, event *xiaoai.Event) {
	var data xiaoai.KWSEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		b.logger.Error("Parse KWS event failed", zap.Error(err))
		return
	}

	if data.Keyword != "" {
		b.logger.Info("Wake word detected (KWS)", zap.String("keyword", data.Keyword))
		go b.startVoiceInteraction(ctx, client, sess)
	}
}

// handleInstructionEvent 处理小爱原生STT事件
func (b *Bridge) handleInstructionEvent(ctx context.Context, client *xiaoai.Client, sess *session.Session, event *xiaoai.Event) {
	var data xiaoai.InstructionEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		b.logger.Debug("Parse instruction event failed", zap.Error(err))
		return
	}

	// 检测唤醒：namespace=SpeechRecognizer, name=RecognizeResult
	if data.Header.Namespace == "SpeechRecognizer" && data.Header.Name == "RecognizeResult" {
		text := ""
		if len(data.Payload.Results) > 0 {
			text = data.Payload.Results[0].Text
		}

		// 唤醒条件：text 为空 或 is_vad_begin 为 true
		if text == "" || data.Payload.IsVadBegin {
			b.logger.Info("Wake word detected (instruction)",
				zap.String("text", text),
				zap.Bool("is_vad_begin", data.Payload.IsVadBegin))

			// 只有在空闲状态才启动新会话
			if sess.GetState() == session.StateIdle {
				go b.startVoiceInteraction(ctx, client, sess)
			}
		}
	}
}

// handlePlayingEvent 处理播放状态事件
func (b *Bridge) handlePlayingEvent(ctx context.Context, client *xiaoai.Client, sess *session.Session, event *xiaoai.Event) {
	var data xiaoai.PlayingEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		b.logger.Debug("Parse playing event failed", zap.Error(err))
		return
	}

	b.logger.Debug("Playing status changed", zap.String("status", data.Status))
}

// startVoiceInteraction 启动完整语音交互流程
func (b *Bridge) startVoiceInteraction(ctx context.Context, client *xiaoai.Client, sess *session.Session) {
	// 重置会话
	sess = b.sessionMgr.ResetSession(client.ID)
	defer func() {
		sess.SetState(session.StateIdle)
	}()

	b.logger.Info("Starting voice interaction", zap.String("session", sess.ID))

	// 1. 中断小爱原生处理
	if err := client.AbortXiaoAI(); err != nil {
		b.logger.Error("Abort XiaoAI failed", zap.Error(err))
	}

	// 2. 播放唤醒提示
	if sess.Config().Prompts.Wakeup != "" {
		if err := client.PlayText(sess.Config().Prompts.Wakeup); err != nil {
			b.logger.Error("Play wakeup prompt failed", zap.Error(err))
		}
		time.Sleep(500 * time.Millisecond) // 等待 TTS 开始
	}

	// 3. 多轮对话循环
	for {
		select {
		case <-sess.Context().Done():
			b.logger.Info("Session context cancelled")
			return
		default:
		}

		// 处理一轮对话
		shouldContinue := b.processOneRound(ctx, client, sess)
		if !shouldContinue {
			break
		}

		// 检查是否支持多轮
		if !sess.IsMultiTurnEnabled() {
			break
		}
	}

	// 4. 会话结束
	b.logger.Info("Voice interaction ended",
		zap.String("session", sess.ID),
		zap.Int("rounds", sess.RoundCount))
}

// processOneRound 处理一轮对话
func (b *Bridge) processOneRound(ctx context.Context, client *xiaoai.Client, sess *session.Session) bool {
	cfg := sess.Config()
	
	// 记录整轮对话开始时间
	roundStart := time.Now()
	var waitSpeechDuration, pipelineDuration, ttsDuration time.Duration

	// 1. 等待用户开始说话
	sess.SetState(session.StateListening)
	b.logger.Debug("Waiting for speech...")

	// 清空之前的音频缓冲
	sess.DrainAudio()

	// 启动录音
	audioConfig := &xiaoai.AudioConfigPayload{
		Channels:      1,
		BitsPerSample: 16,
		SampleRate:    16000,
		BufferSize:    960,  // 30ms @ 16kHz
		PeriodSize:    480,
	}
	if err := client.StartRecording(audioConfig); err != nil {
		b.logger.Error("Start recording failed", zap.Error(err))
		return false
	}

	// 等待用户说话
	waitTimeout := cfg.WaitSpeechTimeout
	if sess.RoundCount > 0 {
		waitTimeout = cfg.PostResponseWait
	}

	waitStart := time.Now()
	speechStarted := sess.WaitForSpeech(waitTimeout)
	waitSpeechDuration = time.Since(waitStart)

	if !speechStarted {
		// 超时，结束会话
		client.StopRecording()
		if cfg.Prompts.Timeout != "" {
			client.PlayText(cfg.Prompts.Timeout)
		}
		b.logger.Info("Speech wait timeout",
			zap.Duration("wait_duration", waitSpeechDuration))
		return false
	}

	b.logger.Debug("Speech detected",
		zap.Duration("wait_duration", waitSpeechDuration))

	// 2. 录音并发送到 HA
	sess.SetState(session.StateRecording)
	b.logger.Debug("Recording started")

	// 创建 HA Pipeline 客户端
	haClient := homeassistant.NewPipelineClient(&b.config.HomeAssistant, b.logger)
	if err := haClient.Connect(ctx); err != nil {
		b.logger.Error("Connect to HA failed", zap.Error(err))
		client.StopRecording()
		return false
	}
	defer haClient.Close()

	// 3. 运行 Pipeline
	sess.SetState(session.StateProcessing)
	pipelineStart := time.Now()

	pipelineCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := haClient.RunPipeline(pipelineCtx, sess.AudioStream(), sess.AudioEndChan(), sess.ConversationID)
	pipelineDuration = time.Since(pipelineStart)

	// 停止录音
	client.StopRecording()

	if err != nil {
		b.logger.Error("Pipeline failed",
			zap.Error(err),
			zap.Duration("pipeline_duration", pipelineDuration))
		return false
	}

	if result.Error != nil {
		b.logger.Error("Pipeline result error",
			zap.Error(result.Error),
			zap.Duration("pipeline_duration", pipelineDuration))
		return false
	}

	// 更新会话 ID
	if result.ConversationID != "" {
		sess.ConversationID = result.ConversationID
	}

	b.logger.Info("Pipeline completed",
		zap.String("stt", result.STTText),
		zap.String("response", result.Response),
		zap.Duration("pipeline_duration", pipelineDuration))

	// 检查是否为结束意图
	if sess.IsEndIntent(result.STTText) {
		if cfg.Prompts.Goodbye != "" {
			client.PlayText(cfg.Prompts.Goodbye)
		}
		totalDuration := time.Since(roundStart)
		b.logger.Info("Round completed (goodbye)",
			zap.Int("round", sess.RoundCount+1),
			zap.Duration("total", totalDuration),
			zap.Duration("wait_speech", waitSpeechDuration),
			zap.Duration("pipeline", pipelineDuration))
		return false
	}

	// 4. 播放 TTS 响应
	if len(result.TTSAudio) > 0 {
		sess.SetState(session.StateSpeaking)
		ttsStart := time.Now()
		b.playTTSAudio(client, result.TTSAudio)
		ttsDuration = time.Since(ttsStart)
	}

	sess.IncrementRound()
	totalDuration := time.Since(roundStart)

	// 输出完整的耗时统计
	b.logger.Info("Round completed",
		zap.Int("round", sess.RoundCount),
		zap.Duration("total", totalDuration),
		zap.Duration("wait_speech", waitSpeechDuration),
		zap.Duration("pipeline", pipelineDuration),
		zap.Duration("tts_play", ttsDuration),
		zap.String("stt", result.STTText))

	return true
}

// playTTSAudio 播放 TTS 音频
func (b *Bridge) playTTSAudio(client *xiaoai.Client, audio []byte) {
	// 启动播放
	playConfig := &xiaoai.AudioConfigPayload{
		Channels:      1,
		BitsPerSample: 16,
		SampleRate:    24000, // Piper 输出采样率
		BufferSize:    2400,
		PeriodSize:    1200,
	}

	if err := client.StartPlay(playConfig); err != nil {
		b.logger.Error("Start play failed", zap.Error(err))
		return
	}

	// 分块发送音频
	chunkSize := 4800 // 100ms @ 24kHz, 16bit

	for i := 0; i < len(audio); i += chunkSize {
		end := i + chunkSize
		if end > len(audio) {
			end = len(audio)
		}

		if err := client.SendStream(xiaoai.StreamTagPlay, audio[i:end]); err != nil {
			b.logger.Error("Send audio failed", zap.Error(err))
			break
		}

		// 控制发送速率，避免缓冲溢出
		time.Sleep(80 * time.Millisecond)
	}

	client.StopPlay()
}
