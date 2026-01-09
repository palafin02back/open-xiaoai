// Package xiaoai 定义 Open-XiaoAI 通信协议
package xiaoai

import "encoding/json"

// MessageType 消息类型
type MessageType string

const (
	MessageTypeRequest  MessageType = "Request"
	MessageTypeResponse MessageType = "Response"
	MessageTypeEvent    MessageType = "Event"
	MessageTypeStream   MessageType = "Stream"
)

// AppMessage 通用消息包装，支持四种消息类型
type AppMessage struct {
	Request  *Request  `json:"Request,omitempty"`
	Response *Response `json:"Response,omitempty"`
	Event    *Event    `json:"Event,omitempty"`
	Stream   *Stream   `json:"Stream,omitempty"`
}

// Request RPC 请求
type Request struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response RPC 响应
type Response struct {
	ID   string          `json:"id"`
	Code *int            `json:"code,omitempty"`
	Msg  *string         `json:"msg,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Event 事件通知（单向）
type Event struct {
	ID    string          `json:"id"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Stream 音频流
type Stream struct {
	ID    string          `json:"id"`
	Tag   string          `json:"tag"` // "record" | "play"
	Bytes []byte          `json:"bytes"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// 事件类型常量
const (
	EventKWS         = "kws"         // 唤醒词检测
	EventInstruction = "instruction" // 语音识别指令（来自小爱原生STT）
	EventPlaying     = "playing"     // 播放状态
)

// 命令类型常量
const (
	CmdGetVersion     = "get_version"
	CmdStartPlay      = "start_play"
	CmdStopPlay       = "stop_play"
	CmdStartRecording = "start_recording"
	CmdStopRecording  = "stop_recording"
	CmdRunShell       = "run_shell"
)

// 流标签
const (
	StreamTagRecord = "record" // 录音流
	StreamTagPlay   = "play"   // 播放流
)

// AudioConfigPayload 音频配置 Payload
type AudioConfigPayload struct {
	PCM           string `json:"pcm,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	BitsPerSample int    `json:"bits_per_sample,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	PeriodSize    int    `json:"period_size,omitempty"`
	BufferSize    int    `json:"buffer_size,omitempty"`
}

// KWSEventData KWS 事件数据
type KWSEventData struct {
	Started bool   `json:"Started,omitempty"`
	Keyword string `json:"Keyword,omitempty"`
}

// InstructionEventData instruction 事件数据（来自小爱原生STT）
type InstructionEventData struct {
	Header  InstructionHeader  `json:"header"`
	Payload InstructionPayload `json:"payload"`
}

// InstructionHeader instruction 事件头
type InstructionHeader struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	DialogID  string `json:"dialog_id,omitempty"`
}

// InstructionPayload instruction 事件负载
type InstructionPayload struct {
	IsFinal     bool                `json:"is_final"`
	IsVadBegin  bool                `json:"is_vad_begin"`
	Results     []RecognizeResult   `json:"results,omitempty"`
}

// RecognizeResult 识别结果
type RecognizeResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

// PlayingEventData 播放状态事件数据
type PlayingEventData struct {
	Status string `json:"status"` // "playing" | "paused" | "idle"
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(id string, data interface{}) *Response {
	code := 0
	msg := "success"
	
	var dataJSON json.RawMessage
	if data != nil {
		dataJSON, _ = json.Marshal(data)
	}
	
	return &Response{
		ID:   id,
		Code: &code,
		Msg:  &msg,
		Data: dataJSON,
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(id string, errMsg string) *Response {
	code := -1
	return &Response{
		ID:   id,
		Code: &code,
		Msg:  &errMsg,
	}
}
