// Package homeassistant 提供 Assist Pipeline 客户端
package homeassistant

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/open-xiaoai/ha-bridge/internal/config"
)

// PipelineClient Assist Pipeline 客户端
type PipelineClient struct {
	*Client
	sttHandlerID int
}

// NewPipelineClient 创建 Pipeline 客户端
func NewPipelineClient(cfg *config.HAConfig, logger *zap.Logger) *PipelineClient {
	return &PipelineClient{
		Client: NewClient(cfg, logger),
	}
}

// RunPipeline 运行语音管道
// audioStream: 音频数据通道
// audioEndChan: 音频结束信号通道（VAD 检测到说话结束时关闭）
// 返回 Pipeline 执行结果
func (p *PipelineClient) RunPipeline(ctx context.Context, audioStream <-chan []byte, audioEndChan <-chan struct{}, conversationID string) (*PipelineResult, error) {
	if !p.IsConnected() {
		return nil, fmt.Errorf("not connected to Home Assistant")
	}

	result := &PipelineResult{
		ConversationID: conversationID,
	}

	// 启动 Pipeline
	msgID := p.NextMsgID()
	pipelineCmd := map[string]interface{}{
		"id":          msgID,
		"type":        "assist_pipeline/run",
		"start_stage": "stt",
		"end_stage":   "tts",
		"input": map[string]interface{}{
			"sample_rate": 16000,
		},
	}

	// 如果有会话 ID，添加到请求中
	if conversationID != "" {
		pipelineCmd["conversation_id"] = conversationID
	}

	if err := p.WriteJSON(pipelineCmd); err != nil {
		return nil, fmt.Errorf("send pipeline command failed: %w", err)
	}

	// 处理 Pipeline 事件
	audioSendStarted := false

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var msg map[string]interface{}
		if err := p.ReadJSON(&msg); err != nil {
			return nil, fmt.Errorf("read message failed: %w", err)
		}

		msgType, _ := msg["type"].(string)

		// 检查是否是我们的消息
		if id, ok := msg["id"].(float64); ok && int(id) != msgID {
			continue
		}

		if msgType == "result" {
			// Pipeline 完成或出错
			if success, ok := msg["success"].(bool); ok && !success {
				if errData, ok := msg["error"].(map[string]interface{}); ok {
					errMsg, _ := errData["message"].(string)
					result.Error = fmt.Errorf("pipeline error: %s", errMsg)
				}
				return result, nil
			}
		}

		if msgType == "event" {
			event, _ := msg["event"].(map[string]interface{})
			eventType, _ := event["type"].(string)
			eventData, _ := event["data"].(map[string]interface{})

			switch eventType {
			case "run-start":
				p.logger.Debug("Pipeline run started")

			case "stt-start":
				// 获取 handler ID 并开始发送音频
				if handlerID, ok := eventData["stt_binary_handler_id"].(float64); ok {
					p.sttHandlerID = int(handlerID)
					p.logger.Debug("STT started", zap.Int("handler_id", p.sttHandlerID))

					// 启动音频发送协程
					if !audioSendStarted {
						audioSendStarted = true
						go p.sendAudioStream(ctx, audioStream, audioEndChan)
					}
				}

			case "stt-vad-start":
				p.logger.Debug("VAD detected speech start")

			case "stt-vad-end":
				p.logger.Debug("VAD detected speech end")

			case "stt-end":
				// STT 完成
				if sttOutput, ok := eventData["stt_output"].(map[string]interface{}); ok {
					result.STTText, _ = sttOutput["text"].(string)
					p.logger.Info("STT result", zap.String("text", result.STTText))
				}

			case "intent-start":
				p.logger.Debug("Intent processing started")

			case "intent-end":
				// Intent 处理完成
				if intentOutput, ok := eventData["intent_output"].(map[string]interface{}); ok {
					if response, ok := intentOutput["response"].(map[string]interface{}); ok {
						if speech, ok := response["speech"].(map[string]interface{}); ok {
							if plain, ok := speech["plain"].(map[string]interface{}); ok {
								result.Response, _ = plain["speech"].(string)
							}
						}
					}
					// 获取会话 ID
					if convID, ok := intentOutput["conversation_id"].(string); ok {
						result.ConversationID = convID
					}
				}
				p.logger.Debug("Intent completed", zap.String("response", result.Response))

			case "tts-start":
				p.logger.Debug("TTS started")

			case "tts-end":
				// TTS 完成
				if ttsOutput, ok := eventData["tts_output"].(map[string]interface{}); ok {
					if url, ok := ttsOutput["url"].(string); ok {
						result.TTSURL = url
						// 下载 TTS 音频
						audio, err := p.downloadTTSAudio(url)
						if err != nil {
							p.logger.Error("Download TTS audio failed", zap.Error(err))
						} else {
							result.TTSAudio = audio
							p.logger.Debug("TTS audio downloaded", zap.Int("size", len(audio)))
						}
					}
				}

			case "run-end":
				p.logger.Debug("Pipeline run completed")
				return result, nil

			case "error":
				errMsg, _ := eventData["message"].(string)
				result.Error = fmt.Errorf("pipeline error: %s", errMsg)
				p.logger.Error("Pipeline error", zap.String("message", errMsg))
				return result, nil
			}
		}
	}
}

// sendAudioStream 发送音频流到 HA
func (p *PipelineClient) sendAudioStream(ctx context.Context, audioStream <-chan []byte, audioEndChan <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			// 发送结束标记
			p.WriteMessage(websocket.BinaryMessage, []byte{byte(p.sttHandlerID)})
			return
		case <-audioEndChan:
			// VAD 检测到语音结束，发送结束标记
			p.logger.Debug("Audio end signal received, sending end marker")
			p.WriteMessage(websocket.BinaryMessage, []byte{byte(p.sttHandlerID)})
			return
		case audio, ok := <-audioStream:
			if !ok {
				// 通道关闭，发送结束标记
				p.WriteMessage(websocket.BinaryMessage, []byte{byte(p.sttHandlerID)})
				return
			}
			// 发送格式: [handler_id_byte][audio_data]
			data := append([]byte{byte(p.sttHandlerID)}, audio...)
			if err := p.WriteMessage(websocket.BinaryMessage, data); err != nil {
				p.logger.Error("Send audio failed", zap.Error(err))
				return
			}
		}
	}
}

// SendAudioEnd 发送音频结束标记
func (p *PipelineClient) SendAudioEnd() error {
	return p.WriteMessage(websocket.BinaryMessage, []byte{byte(p.sttHandlerID)})
}

// downloadTTSAudio 下载 TTS 音频
func (p *PipelineClient) downloadTTSAudio(url string) ([]byte, error) {
	// 拼接完整 URL
	fullURL := url
	if len(url) > 0 && url[0] == '/' {
		fullURL = p.config.URL + url
	}

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.config.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// ConversationProcess 调用对话处理 API
func (p *PipelineClient) ConversationProcess(ctx context.Context, text, conversationID string) (*ConversationResponse, error) {
	msgID := p.NextMsgID()
	
	req := map[string]interface{}{
		"id":   msgID,
		"type": "conversation/process",
		"text": text,
	}
	
	if conversationID != "" {
		req["conversation_id"] = conversationID
	}

	if err := p.WriteJSON(req); err != nil {
		return nil, fmt.Errorf("send conversation request failed: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var msg map[string]interface{}
		if err := p.ReadJSON(&msg); err != nil {
			return nil, fmt.Errorf("read message failed: %w", err)
		}

		if id, ok := msg["id"].(float64); ok && int(id) == msgID {
			if msgType, _ := msg["type"].(string); msgType == "result" {
				if success, ok := msg["success"].(bool); ok && success {
					if result, ok := msg["result"].(map[string]interface{}); ok {
						resp := &ConversationResponse{}
						// 解析响应
						if response, ok := result["response"].(map[string]interface{}); ok {
							if speech, ok := response["speech"].(map[string]interface{}); ok {
								if plain, ok := speech["plain"].(map[string]interface{}); ok {
									resp.Response.Speech.Plain.Speech, _ = plain["speech"].(string)
								}
							}
						}
						if convID, ok := result["conversation_id"].(string); ok {
							resp.ConversationID = convID
						}
						return resp, nil
					}
				} else {
					if errData, ok := msg["error"].(map[string]interface{}); ok {
						errMsg, _ := errData["message"].(string)
						return nil, fmt.Errorf("conversation error: %s", errMsg)
					}
				}
			}
		}
	}
}
