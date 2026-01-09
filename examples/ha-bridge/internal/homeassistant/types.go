// Package homeassistant 提供 HA 类型定义
package homeassistant

// PipelineResult Pipeline 执行结果
type PipelineResult struct {
	STTText        string // 语音识别文本
	Response       string // 对话响应文本
	TTSAudio       []byte // TTS 音频数据
	TTSURL         string // TTS 音频 URL
	ConversationID string // 会话 ID
	Error          error  // 错误信息
}

// PipelineEvent Pipeline 事件
type PipelineEvent struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data,omitempty"`
}

// ConversationRequest 对话请求
type ConversationRequest struct {
	Text           string `json:"text"`
	Language       string `json:"language,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
}

// ConversationResponse 对话响应
type ConversationResponse struct {
	Response struct {
		ResponseType string `json:"response_type"`
		Speech       struct {
			Plain struct {
				Speech    string      `json:"speech"`
				ExtraData interface{} `json:"extra_data"`
			} `json:"plain"`
		} `json:"speech"`
		Language string `json:"language"`
		Data     struct {
			Targets []interface{} `json:"targets"`
			Success []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"success"`
			Failed []interface{} `json:"failed"`
		} `json:"data"`
	} `json:"response"`
	ConversationID string `json:"conversation_id"`
}

// TTSRequest TTS 请求
type TTSRequest struct {
	Message  string `json:"message"`
	Platform string `json:"platform,omitempty"`
	Language string `json:"language,omitempty"`
}

// TTSResponse TTS 响应
type TTSResponse struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

// STTResponse STT 响应
type STTResponse struct {
	Text    string `json:"text"`
	Success bool   `json:"success"`
}
