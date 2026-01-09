// Package session 提供会话状态定义
package session

// State 会话状态
type State int

const (
	// StateIdle 空闲状态
	StateIdle State = iota
	// StateListening 等待用户说话
	StateListening
	// StateRecording 正在录音
	StateRecording
	// StateProcessing 处理中（STT/Intent）
	StateProcessing
	// StateSpeaking 正在播放响应
	StateSpeaking
	// StateEnding 会话结束中
	StateEnding
)

// String 返回状态的字符串表示
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateListening:
		return "listening"
	case StateRecording:
		return "recording"
	case StateProcessing:
		return "processing"
	case StateSpeaking:
		return "speaking"
	case StateEnding:
		return "ending"
	default:
		return "unknown"
	}
}
