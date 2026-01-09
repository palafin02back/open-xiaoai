# Open-XiaoAI HA Bridge - Go 语言技术方案

## 项目概述

### 目标

开发一个 Go 语言编写的 Open-XiaoAI Server，作为小爱音箱与 Home Assistant 之间的桥梁，实现：

1. **低延时语音交互**：流式音频处理，端到端延时 < 1.5s
2. **智能家居控制**：通过 HA Assist Pipeline 实现本地化控制
3. **高性能**：Go 语言的并发特性保证高吞吐和低延时
4. **易部署**：单二进制文件，支持 Docker 部署

### 项目命名

```
open-xiaoai-ha-bridge
```

---

## 系统架构

### 整体架构图

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         小爱音箱 (LX06/OH2P)                             │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────────────────────┐  │
│  │  麦克风采集   │    │ 原生唤醒词   │    │      扬声器播放           │  │
│  │  16kHz PCM   │    │ "小爱同学"   │    │  PCM Stream (24kHz)      │  │
│  └──────┬───────┘    └──────┬───────┘    └────────────▲──────────────┘  │
│         │                   │                         │                  │
│         └───────────────────┼─────────────────────────┘                  │
│                             │                                            │
│           ┌─────────────────▼─────────────────┐                          │
│           │        Open-XiaoAI Client         │                          │
│           │            (Rust)                 │                          │
│           └─────────────────┬─────────────────┘                          │
│                             │ WebSocket (ws://server:4399)               │
└─────────────────────────────┼────────────────────────────────────────────┘
                              │
               ═══════════════╪═══════════════  局域网
                              │
┌─────────────────────────────┼────────────────────────────────────────────┐
│                             │                                            │
│           ┌─────────────────▼─────────────────┐                          │
│           │     Open-XiaoAI HA Bridge         │                          │
│           │           (Go)                    │                          │
│           │                                   │                          │
│           │  ┌─────────────────────────────┐  │                          │
│           │  │      Client Manager         │  │                          │
│           │  │  (多设备连接管理)            │  │                          │
│           │  └─────────────┬───────────────┘  │                          │
│           │                │                  │                          │
│           │  ┌─────────────▼───────────────┐  │                          │
│           │  │     Message Router          │  │                          │
│           │  │  (消息路由与分发)            │  │                          │
│           │  └─────────────┬───────────────┘  │                          │
│           │                │                  │                          │
│           │  ┌─────────────▼───────────────┐  │                          │
│           │  │     Session Manager         │  │                          │
│           │  │  (会话状态管理)              │  │                          │
│           │  └─────────────┬───────────────┘  │                          │
│           │                │                  │                          │
│           │  ┌─────────────▼───────────────┐  │                          │
│           │  │   HA Pipeline Client        │  │                          │
│           │  │  (Assist Pipeline 客户端)   │  │                          │
│           │  └─────────────────────────────┘  │                          │
│           │                                   │                          │
│           └─────────────────┬─────────────────┘                          │
│                             │                                            │
│        ┌────────────────────┼────────────────────┐                       │
│        │                    │                    │                       │
│        ▼                    ▼                    ▼                       │
│  ┌───────────┐       ┌───────────┐       ┌───────────┐                  │
│  │  Whisper  │       │   HA      │       │  Piper    │                  │
│  │   STT     │       │  Core     │       │   TTS     │                  │
│  │ (Wyoming) │       │(Intents)  │       │ (Wyoming) │                  │
│  └───────────┘       └───────────┘       └───────────┘                  │
│                                                                          │
│                       Home Assistant                                     │
└──────────────────────────────────────────────────────────────────────────┘
```

### 数据流详解

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           语音交互完整流程                               │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  1. 唤醒阶段                                                            │
│     ┌──────────┐    KWS Event     ┌──────────┐                         │
│     │ 小爱音箱  │ ──────────────► │ HA Bridge │                         │
│     │(本地KWS) │                  │          │                         │
│     └──────────┘                  └────┬─────┘                         │
│                                        │                                │
│  2. 录音阶段                           │ Start Recording Cmd            │
│     ┌──────────┐    Audio Stream  ┌────▼─────┐                         │
│     │ 小爱音箱  │ ──────────────► │ HA Bridge │                         │
│     │  (Mic)   │    PCM 16kHz    │          │                         │
│     └──────────┘                  └────┬─────┘                         │
│                                        │                                │
│  3. STT 阶段                           │ Audio Stream                   │
│     ┌──────────┐                  ┌────▼─────┐                         │
│     │ HA Bridge │ ──────────────► │ HA Assist│                         │
│     │          │   WebSocket      │ Pipeline │                         │
│     └──────────┘                  └────┬─────┘                         │
│                                        │ STT Result                     │
│  4. Intent 处理                        │                                │
│     ┌──────────┐    Text          ┌────▼─────┐                         │
│     │ HA Assist │ ──────────────► │ Convers- │                         │
│     │ Pipeline │                  │  ation   │                         │
│     └──────────┘                  └────┬─────┘                         │
│                                        │ Response                       │
│  5. TTS 阶段                           │                                │
│     ┌──────────┐    Audio         ┌────▼─────┐                         │
│     │   Piper  │ ◄─────────────── │ HA Assist│                         │
│     │   TTS    │   Stream         │ Pipeline │                         │
│     └────┬─────┘                  └──────────┘                         │
│          │                                                              │
│  6. 播放阶段                                                            │
│     ┌────▼─────┐    Audio Stream  ┌──────────┐                         │
│     │ HA Bridge │ ──────────────► │ 小爱音箱  │                         │
│     │          │   PCM 24kHz     │ (Speaker)│                         │
│     └──────────┘                  └──────────┘                         │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 技术评估与方案选择

### 1. 小爱音箱原生 KWS/VAD 机制分析

#### 1.1 原生系统工作流程

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    小爱音箱原生语音系统                                  │
│                                                                         │
│  麦克风 ──► 原生 KWS ──► 原生 VAD ──► 原生 STT ──► mico_aivs_lab        │
│          ("小爱同学")   (检测说话)   (云端识别)    (日志输出)            │
│                                                                         │
│  日志路径: /tmp/mico_aivs_lab/instruction.log                           │
└─────────────────────────────────────────────────────────────────────────┘
```

**原生系统特点**：
- **KWS**：小爱自带的唤醒词检测（"小爱同学"），硬件级别优化（DSP 加速），非常灵敏
- **VAD**：小爱原生包含 VAD，通过 `is_vad_begin` 标志检测说话开始/结束
- **STT**：小爱原生使用云端语音识别，通过 `RecognizeResult` 事件输出

#### 1.2 instruction 事件数据结构

```json
{
  "header": {
    "namespace": "SpeechRecognizer",
    "name": "RecognizeResult",
    "dialog_id": "xxx"
  },
  "payload": {
    "is_final": true,
    "is_vad_begin": false,
    "results": [
      {
        "text": "打开客厅的灯",
        "confidence": 0.95
      }
    ]
  }
}
```

**关键字段说明**：
| 字段 | 说明 |
|------|------|
| `is_vad_begin` | VAD 检测到用户开始说话 |
| `is_final` | 语音识别完成，text 为最终结果 |
| `results[0].text` | 识别出的文字 |

#### 1.3 唤醒模式对比

| 模式 | 触发方式 | 检测位置 | 延时 | 准确率 | 资源占用 |
|------|----------|----------|------|--------|----------|
| **小爱原生唤醒** | 说"小爱同学" | 硬件级（DSP） | ~200ms | ⭐⭐⭐⭐⭐ | 极低 |
| **Sherpa-ONNX 自定义唤醒** | 自定义词（如"你好小智"） | Client 端 CPU | ~500ms+ | ⭐⭐⭐ | 高（20MB+） |

### 2. 唤醒方案选择

#### 2.1 推荐方案：小爱原生唤醒 + Bridge VAD + HA STT

**核心思路**：
- ✅ 利用小爱原生 KWS（"小爱同学"），无需 Sherpa-ONNX
- ✅ 唤醒后立即中断小爱原生处理 (`abort_xiaoai`)
- ✅ 在 Go Bridge 端做 VAD 检测
- ✅ 使用 HA Whisper 做 STT（本地化、更准确）

**放弃 Sherpa-ONNX 的原因**：
1. 小爱音箱硬件性能有限（128-256MB 内存），运行效果不佳
2. 小爱原生 KWS 已经非常灵敏，无需替换
3. 减少系统复杂度和资源占用

#### 2.2 完整交互流程

```
┌──────────────────────────────────────────────────────────────────────┐
│                          完整语音交互流程                             │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│ 1. 唤醒阶段 (小爱原生 KWS)                                           │
│    用户说 "小爱同学" → 小爱原生 KWS 检测                              │
│                     → instruction 事件 (is_vad_begin 或 空 text)     │
│                     → Bridge 收到事件                                │
│                                                                      │
│ 2. 接管阶段 (关键步骤)                                               │
│    Bridge 收到唤醒:                                                  │
│    └──► 调用 abort_xiaoai() 中断小爱原生处理                         │
│    └──► 调用 start_recording() 启动麦克风录音                        │
│    └──► 可选: 播放提示音 "在呢" (play_text)                          │
│                                                                      │
│ 3. 录音 + VAD (Bridge 端)                                            │
│    音频流 → Bridge VAD 检测                                          │
│           → 有语音: 缓冲并流式发送到 HA                              │
│           → 静音超时: 停止录音，发送结束标记                          │
│                                                                      │
│ 4. STT + Intent + TTS (HA 端)                                        │
│    音频流 → HA Whisper STT                                           │
│          → HA Conversation (Intent 匹配)                             │
│          → HA Piper TTS                                              │
│          → 音频流返回                                                 │
│                                                                      │
│ 5. 播放响应 (小爱音箱)                                                │
│    Bridge 收到 TTS 音频 → start_play()                               │
│                        → 流式发送音频                                 │
│                        → stop_play()                                 │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

#### 2.3 唤醒检测逻辑

根据 `instruction` 事件判断唤醒：

```go
// 检测唤醒的条件:
// 1. namespace = "SpeechRecognizer" AND name = "RecognizeResult"
// 2. text 为空字符串 OR is_vad_begin = true

func (b *Bridge) isWakeupEvent(line *InstructionLine) bool {
    if line.Header.Namespace != "SpeechRecognizer" ||
       line.Header.Name != "RecognizeResult" {
        return false
    }
    
    text := ""
    if len(line.Payload.Results) > 0 {
        text = line.Payload.Results[0].Text
    }
    
    // 唤醒条件：text 为空 或 is_vad_begin 为 true
    return text == "" || line.Payload.IsVadBegin
}
```

### 3. VAD 方案选择

#### 3.1 VAD 位置对比

| VAD 位置 | 优点 | 缺点 | 推荐度 |
|----------|------|------|--------|
| **小爱原生 VAD** | 已有、无需额外实现 | 需等待小爱云端处理完成 | ⭐⭐ |
| **Go Bridge VAD** | 完全控制、可配置阈值、低延时 | 需要实现 | ⭐⭐⭐⭐⭐ |
| **HA 端 VAD** | 集中管理 | 增加网络往返延时 | ⭐⭐⭐ |

#### 3.2 推荐方案：Go Bridge 端 Silero VAD

**原因**：
1. 唤醒后立即中断小爱原生处理，需要自己做 VAD
2. Bridge 端 VAD 可以减少无效音频传输
3. Silero VAD 准确率高（87.7% vs WebRTC 50%），噪声鲁棒性好
4. 有成熟的 Go + ONNX Runtime 实现

**实现方案**：使用 Silero VAD (基于 ONNX)

```go
// Bridge 端 VAD 配置
type VADConfig struct {
    ModelPath        string  // silero_vad.onnx 模型路径
    SpeechThreshold  float32 // 语音概率阈值 (0-1)，推荐 0.5
    SilenceTimeout   int     // 静音超时（毫秒）
    MinSpeechTime    int     // 最小说话时间（毫秒）
    PreBufferFrames  int     // 预缓冲帧数（避免丢失首字）
}
```

**依赖**：
- `github.com/streamer45/silero-vad-go` - Go 语言绑定
- ONNX Runtime (~50MB)
- `silero_vad.onnx` 模型 (~2MB)

**资源占用**：
| 资源 | 预估值 |
|------|--------|
| 内存 | ~50-100MB |
| CPU | <5% |
| 延迟 | <5ms/帧 |

### 4. 关键问题 Q&A

| 问题 | 结论 |
|------|------|
| **是否需要 Sherpa-ONNX KWS？** | ❌ 不需要。如不需自定义唤醒词，使用小爱原生 KWS 即可 |
| **使用哪个唤醒词？** | "小爱同学"（小爱原生，灵敏度最高） |
| **VAD 放在哪里？** | ✅ Go Bridge 端，使用 Silero VAD (ONNX) |
| **STT 用哪个？** | ✅ HA Whisper，不用小爱云端 |
| **唤醒后的关键步骤？** | 立即调用 `abort_xiaoai()` 中断小爱原生处理 |

### 5. 扩展：自定义唤醒词支持（可选）

如果未来需要支持自定义唤醒词，有两种方案：

| 方案 | 实现位置 | 优点 | 缺点 |
|------|----------|------|------|
| **Client 端 Sherpa-ONNX** | 小爱音箱 | 本地化 | 资源占用高、准确率有限 |
| **Server 端 FunASR/Sherpa** | Go Bridge 或独立服务 | 模型更大更准 | 需持续流式传输音频 |

若需 Server 端 KWS，推荐：
- [FunASR](https://github.com/modelscope/FunASR) - 阿里开源，支持流式
- [Sherpa-ONNX Server](https://github.com/k2-fsa/sherpa-onnx) - 可作为 WebSocket 服务

---

## 多轮对话设计

### 1. 需求概述

实现一次唤醒后支持多轮连续对话：

| 功能 | 说明 |
|------|------|
| **多轮对话** | 一次唤醒，连续多轮问答 |
| **等待响应** | 唤醒后等待用户一段时间（可配置） |
| **退出提醒** | 超时或主动结束时播放提示语 |
| **上下文保持** | 通过 HA conversation_id 保持对话上下文 |

### 2. 会话状态机

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        多轮对话状态机                                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌──────────┐  唤醒事件   ┌──────────┐                                 │
│  │  空闲     │ ─────────► │ 等待用户  │  ◄─────────────────┐            │
│  │  IDLE    │            │ LISTENING │                    │            │
│  └──────────┘            └────┬──────┘                    │            │
│       ▲                       │                           │            │
│       │                       │ 检测到语音                 │            │
│       │                       ▼                           │            │
│       │               ┌──────────┐                        │            │
│       │               │ 录音中    │                        │            │
│       │               │RECORDING │                        │            │
│       │               └────┬─────┘                        │            │
│       │                    │                              │            │
│       │                    │ 语音结束                      │            │
│       │                    ▼                              │            │
│       │               ┌──────────┐                        │            │
│       │               │ 处理中    │                        │            │
│       │               │PROCESSING│                        │            │
│       │               └────┬─────┘                        │            │
│       │                    │                              │            │
│       │                    │ STT+Intent 完成              │ 继续对话   │
│       │                    ▼                              │ (非结束意图)│
│       │               ┌──────────┐                        │            │
│       │               │ 播放响应  │ ──────────────────────┘            │
│       │               │ SPEAKING │                                     │
│       │               └────┬─────┘                                     │
│       │                    │                                           │
│       │                    │ 播放完成 + 检测到结束意图                  │
│       │                    │ 或 等待超时                                │
│       │                    ▼                                           │
│       │               ┌──────────┐                                     │
│       │  播放结束语   │ 结束会话  │                                     │
│       └───────────── │ ENDING   │                                     │
│                       └──────────┘                                     │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### 3. 典型交互流程

```
用户: "小爱同学"
系统: "在呢" 【进入 LISTENING，等待用户说话】

用户: "打开客厅的灯"
系统: "好的，客厅灯已打开" 【执行命令，等待继续...】
      ↓
     (等待 3-5 秒)
      ↓
用户: "再把空调打开"                     ← 多轮对话
系统: "好的，空调已打开"
      ↓
     (等待 3-5 秒，用户无响应)
      ↓
     (继续等待，累计 8 秒无响应)
      ↓
系统: "下次需要帮忙再叫我哦~" 【退出会话】  ← 退出提醒
```

### 4. 结束会话触发条件

| 条件 | 说明 | 行为 |
|------|------|------|
| **超时退出** | 等待用户说话超过阈值 | 播放 timeout 提示语 |
| **主动结束** | 用户说"没有了"、"谢谢"、"再见" | 播放 goodbye 提示语 |
| **最大时长** | 会话持续超过上限（防止卡死）| 播放 timeout 提示语 |

### 5. 会话配置

```go
// SessionConfig 会话配置
type SessionConfig struct {
    // 多轮对话开关
    MultiTurnEnabled   bool          `yaml:"multi_turn_enabled"`
    
    // 超时设置
    WaitSpeechTimeout  time.Duration `yaml:"wait_speech_timeout"`   // 等待用户说话超时 (默认 8s)
    PostResponseWait   time.Duration `yaml:"post_response_wait"`    // 响应后等待继续 (默认 4s)
    MaxSessionDuration time.Duration `yaml:"max_session_duration"`  // 最大会话时长 (默认 3分钟)
    
    // 提示语
    Prompts struct {
        Wakeup   string `yaml:"wakeup"`   // 唤醒提示 "在呢"
        Timeout  string `yaml:"timeout"`  // 超时提示 "下次需要帮忙再叫我~"
        Goodbye  string `yaml:"goodbye"`  // 主动结束 "好的，再见~"
        Continue string `yaml:"continue"` // 继续监听 (可选) "还有其他需要帮忙的吗？"
    } `yaml:"prompts"`
    
    // 结束关键词
    EndKeywords []string `yaml:"end_keywords"`
}

// DefaultSessionConfig 默认配置
func DefaultSessionConfig() SessionConfig {
    return SessionConfig{
        MultiTurnEnabled:   true,
        WaitSpeechTimeout:  8 * time.Second,
        PostResponseWait:   4 * time.Second,
        MaxSessionDuration: 3 * time.Minute,
        
        Prompts: struct {
            Wakeup   string `yaml:"wakeup"`
            Timeout  string `yaml:"timeout"`
            Goodbye  string `yaml:"goodbye"`
            Continue string `yaml:"continue"`
        }{
            Wakeup:   "在呢",
            Timeout:  "下次需要帮忙再叫我哦~",
            Goodbye:  "好的，再见~",
            Continue: "",
        },
        
        EndKeywords: []string{
            "没有了", "没了", "谢谢", "再见", 
            "退出", "结束", "好了", "不用了",
        },
    }
}
```

### 6. 会话管理核心逻辑

```go
// Session 会话
type Session struct {
    ID              string
    State           SessionState
    ClientID        string
    ConversationID  string          // HA 会话 ID（保持上下文）
    
    StartTime       time.Time
    LastActiveTime  time.Time
    RoundCount      int             // 对话轮数
    
    config          SessionConfig
    cancelFunc      context.CancelFunc
    speechDetected  chan struct{}   // 语音检测信号
}

// runSessionLoop 会话主循环
func (s *Session) runSessionLoop(ctx context.Context, client *Client) {
    defer s.endSession(client)
    
    // 播放唤醒提示
    if s.config.Prompts.Wakeup != "" {
        client.PlayText(s.config.Prompts.Wakeup)
    }
    
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        
        // 等待用户说话
        s.State = StateListening
        speechStarted := s.waitForSpeech(ctx, s.config.WaitSpeechTimeout)
        
        if !speechStarted {
            // 等待超时，结束会话
            client.PlayText(s.config.Prompts.Timeout)
            return
        }
        
        // 录音并处理
        s.State = StateRecording
        result, err := s.recordAndProcess(ctx, client)
        if err != nil {
            continue
        }
        
        s.RoundCount++
        s.LastActiveTime = time.Now()
        
        // 检查是否为结束意图
        if s.isEndIntent(result.STTText) {
            client.PlayText(s.config.Prompts.Goodbye)
            return
        }
        
        // 播放响应
        s.State = StateSpeaking
        s.playResponse(client, result)
        
        // 多轮模式：继续等待
        if !s.config.MultiTurnEnabled {
            return
        }
    }
}

// waitForSpeech 等待用户开始说话
func (s *Session) waitForSpeech(ctx context.Context, timeout time.Duration) bool {
    timer := time.NewTimer(timeout)
    defer timer.Stop()
    
    select {
    case <-ctx.Done():
        return false
    case <-timer.C:
        return false
    case <-s.speechDetected:
        return true
    }
}

// isEndIntent 检查是否为结束意图
func (s *Session) isEndIntent(text string) bool {
    for _, keyword := range s.config.EndKeywords {
        if strings.Contains(text, keyword) {
            return true
        }
    }
    return false
}
```

### 7. HA 多轮上下文保持

```go
// HA Conversation API 通过 conversation_id 保持上下文

func (b *Bridge) processWithContext(session *Session, text string) (*HAResult, error) {
    result, err := b.haClient.ConversationProcess(context.Background(), &ConversationRequest{
        Text:           text,
        Language:       "zh-CN",
        ConversationID: session.ConversationID, // 传递会话 ID
    })
    
    if err != nil {
        return nil, err
    }
    
    // 保存/更新会话 ID（首次请求后 HA 返回的 ID）
    session.ConversationID = result.ConversationID
    
    return result, nil
}
```

### 8. 交互时序图

```
┌─────────┐        ┌─────────┐        ┌─────────┐        ┌─────────┐
│ 用户    │        │ 小爱音箱 │        │ Bridge  │        │   HA    │
└────┬────┘        └────┬────┘        └────┬────┘        └────┬────┘
     │                  │                  │                  │
     │ 说"小爱同学"     │                  │                  │
     │ ────────────────►│                  │                  │
     │                  │ instruction事件   │                  │
     │                  │ ────────────────►│                  │
     │                  │                  │ 开始会话          │
     │                  │ 播放"在呢"       │                  │
     │ "在呢"           │ ◄────────────────│                  │
     │ ◄────────────────│                  │                  │
     │                  │                  │                  │
     │ 说"打开客厅灯"   │ 音频流           │                  │
     │ ────────────────►│ ────────────────►│ 音频流           │
     │                  │                  │ ────────────────►│
     │                  │                  │ 响应+TTS         │
     │                  │ 播放响应         │ ◄────────────────│
     │ "灯已打开"       │ ◄────────────────│                  │
     │ ◄────────────────│                  │                  │
     │                  │                  │ 等待继续(4秒)    │
     │                  │                  │ ═══════════════  │
     │                  │                  │                  │
     │ 说"空调也打开"   │ 音频流           │ (同会话ID)       │
     │ ────────────────►│ ────────────────►│ ────────────────►│
     │                  │                  │ 响应+TTS         │
     │ "空调已打开"     │ 播放响应         │ ◄────────────────│
     │ ◄────────────────│ ◄────────────────│                  │
     │                  │                  │                  │
     │ (无响应 8秒)     │                  │ 等待超时         │
     │                  │                  │ ═══════════════  │
     │                  │ 播放结束语       │                  │
     │ "下次再叫我~"    │ ◄────────────────│                  │
     │ ◄────────────────│                  │ 结束会话         │
     │                  │                  │                  │
```

### 9. 配置文件示例

```yaml
# config.yaml
session:
  # 多轮对话设置
  multi_turn_enabled: true
  wait_speech_timeout: 8000      # 等待用户说话超时（毫秒）
  post_response_wait: 4000       # 响应后等待继续（毫秒）
  max_session_duration: 180000   # 最大会话时长（毫秒，3分钟）
  
  # 提示语配置
  prompts:
    wakeup: "在呢"
    timeout: "下次需要帮忙再叫我哦~"
    goodbye: "好的，再见~"
    continue: ""  # 空则不播放
  
  # 结束关键词
  end_keywords:
    - "没有了"
    - "没了" 
    - "谢谢"
    - "再见"
    - "退出"
    - "结束"
    - "好了"
    - "不用了"
```

---

## 模块设计

### 项目结构

```
open-xiaoai-ha-bridge/
├── cmd/
│   └── bridge/
│       └── main.go                 # 程序入口
├── internal/
│   ├── config/
│   │   └── config.go              # 配置管理
│   ├── xiaoai/
│   │   ├── server.go              # WebSocket 服务器
│   │   ├── client.go              # 客户端连接管理
│   │   ├── protocol.go            # Open-XiaoAI 协议定义
│   │   └── handler.go             # 消息处理器
│   ├── homeassistant/
│   │   ├── client.go              # HA WebSocket 客户端
│   │   ├── pipeline.go            # Assist Pipeline 管理
│   │   ├── conversation.go        # Conversation API
│   │   └── types.go               # HA 类型定义
│   ├── session/
│   │   ├── manager.go             # 会话管理器
│   │   ├── session.go             # 单个会话
│   │   └── state.go               # 会话状态机
│   ├── audio/
│   │   ├── buffer.go              # 音频缓冲
│   │   ├── vad.go                 # 语音活动检测
│   │   └── resample.go            # 采样率转换
│   └── router/
│       └── router.go              # 消息路由
├── pkg/
│   ├── websocket/
│   │   └── websocket.go           # WebSocket 工具
│   └── logger/
│       └── logger.go              # 日志工具
├── configs/
│   └── config.yaml                # 配置文件模板
├── Dockerfile
├── docker-compose.yaml
├── go.mod
├── go.sum
└── README.md
```

---

## 核心模块详细设计

### 1. 配置管理 (internal/config)

```go
// config.go
package config

import (
    "os"
    "gopkg.in/yaml.v3"
)

type Config struct {
    Server      ServerConfig      `yaml:"server"`
    HomeAssistant HAConfig        `yaml:"homeassistant"`
    Audio       AudioConfig       `yaml:"audio"`
    Session     SessionConfig     `yaml:"session"`
    Log         LogConfig         `yaml:"log"`
}

type ServerConfig struct {
    Host string `yaml:"host" default:"0.0.0.0"`
    Port int    `yaml:"port" default:"4399"`
}

type HAConfig struct {
    URL            string `yaml:"url"`              // http://homeassistant.local:8123
    Token          string `yaml:"token"`            // Long-Lived Access Token
    PipelineID     string `yaml:"pipeline_id"`      // 可选，指定 Pipeline
    ConversationID string `yaml:"conversation_id"`  // 可选，持久会话
}

type AudioConfig struct {
    InputSampleRate  int  `yaml:"input_sample_rate" default:"16000"`
    OutputSampleRate int  `yaml:"output_sample_rate" default:"24000"`
    Channels         int  `yaml:"channels" default:"1"`
    BitsPerSample    int  `yaml:"bits_per_sample" default:"16"`
    VADEnabled       bool `yaml:"vad_enabled" default:"true"`
    VADThreshold     int  `yaml:"vad_threshold" default:"3"`  // WebRTC VAD aggressiveness
    SilenceTimeout   int  `yaml:"silence_timeout" default:"1000"` // ms
}

type SessionConfig struct {
    MaxIdleTime    int `yaml:"max_idle_time" default:"300"`     // 秒
    MaxSessionTime int `yaml:"max_session_time" default:"3600"` // 秒
}

type LogConfig struct {
    Level  string `yaml:"level" default:"info"`
    Format string `yaml:"format" default:"json"` // json | text
}

func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    cfg := &Config{}
    if err := yaml.Unmarshal(data, cfg); err != nil {
        return nil, err
    }
    
    return cfg, nil
}
```

### 2. Open-XiaoAI 协议 (internal/xiaoai)

```go
// protocol.go
package xiaoai

import "encoding/json"

// 消息类型枚举
type MessageType string

const (
    MessageTypeRequest  MessageType = "Request"
    MessageTypeResponse MessageType = "Response"
    MessageTypeEvent    MessageType = "Event"
    MessageTypeStream   MessageType = "Stream"
)

// 通用消息包装
type AppMessage struct {
    Request  *Request  `json:"Request,omitempty"`
    Response *Response `json:"Response,omitempty"`
    Event    *Event    `json:"Event,omitempty"`
    Stream   *Stream   `json:"Stream,omitempty"`
}

// Request: RPC 请求
type Request struct {
    ID      string          `json:"id"`
    Command string          `json:"command"`
    Payload json.RawMessage `json:"payload,omitempty"`
}

// Response: RPC 响应
type Response struct {
    ID   string          `json:"id"`
    Code *int            `json:"code,omitempty"`
    Msg  *string         `json:"msg,omitempty"`
    Data json.RawMessage `json:"data,omitempty"`
}

// Event: 事件通知
type Event struct {
    ID    string          `json:"id"`
    Event string          `json:"event"`
    Data  json.RawMessage `json:"data,omitempty"`
}

// Stream: 音频流
type Stream struct {
    ID    string          `json:"id"`
    Tag   string          `json:"tag"`   // "record" | "play"
    Bytes []byte          `json:"bytes"`
    Data  json.RawMessage `json:"data,omitempty"`
}

// 事件类型
const (
    EventKWS         = "kws"         // 唤醒词检测
    EventInstruction = "instruction" // 语音识别指令
    EventPlaying     = "playing"     // 播放状态
)

// KWS 事件数据
type KWSEventData struct {
    Started bool   `json:"Started,omitempty"`
    Keyword string `json:"Keyword,omitempty"`
}

// 命令类型
const (
    CmdGetVersion     = "get_version"
    CmdStartPlay      = "start_play"
    CmdStopPlay       = "stop_play"
    CmdStartRecording = "start_recording"
    CmdStopRecording  = "stop_recording"
    CmdRunShell       = "run_shell"
)

// 音频配置 Payload
type AudioConfigPayload struct {
    PCM           string `json:"pcm,omitempty"`
    Channels      int    `json:"channels,omitempty"`
    BitsPerSample int    `json:"bits_per_sample,omitempty"`
    SampleRate    int    `json:"sample_rate,omitempty"`
    PeriodSize    int    `json:"period_size,omitempty"`
    BufferSize    int    `json:"buffer_size,omitempty"`
}
```

### 3. WebSocket 服务器 (internal/xiaoai)

```go
// server.go
package xiaoai

import (
    "context"
    "net/http"
    "sync"
    
    "github.com/gorilla/websocket"
    "go.uber.org/zap"
)

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
}

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

func (s *Server) Start(ctx context.Context) error {
    http.HandleFunc("/", s.handleWebSocket)
    
    addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
    s.logger.Info("Starting XiaoAI WebSocket server", zap.String("addr", addr))
    
    server := &http.Server{Addr: addr}
    
    go func() {
        <-ctx.Done()
        server.Shutdown(context.Background())
    }()
    
    return server.ListenAndServe()
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
    
    if s.onClientConnect != nil {
        s.onClientConnect(client)
    }
    
    client.Run(s.handleMessage)
}

func (s *Server) handleMessage(client *Client, msgType int, data []byte) {
    if msgType == websocket.BinaryMessage {
        // 处理 Stream 消息
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
        s.logger.Error("Failed to parse message", zap.Error(err))
        return
    }
    
    if msg.Request != nil {
        s.handleRequest(client, msg.Request)
    } else if msg.Event != nil {
        if s.onEvent != nil {
            s.onEvent(client, msg.Event)
        }
    }
}

func (s *Server) handleRequest(client *Client, req *Request) {
    switch req.Command {
    case CmdGetVersion:
        client.SendResponse(req.ID, 0, "success", json.RawMessage(`"1.0.0"`))
    default:
        s.logger.Warn("Unknown command", zap.String("command", req.Command))
    }
}

// 注册事件回调
func (s *Server) OnClientConnect(fn func(*Client)) {
    s.onClientConnect = fn
}

func (s *Server) OnClientDisconnect(fn func(*Client)) {
    s.onClientDisconnect = fn
}

func (s *Server) OnEvent(fn func(*Client, *Event)) {
    s.onEvent = fn
}

func (s *Server) OnStream(fn func(*Client, *Stream)) {
    s.onStream = fn
}
```

### 4. 客户端连接管理 (internal/xiaoai)

```go
// client.go
package xiaoai

import (
    "encoding/json"
    "sync"
    "time"
    
    "github.com/google/uuid"
    "github.com/gorilla/websocket"
    "go.uber.org/zap"
)

type Client struct {
    ID         string
    conn       *websocket.Conn
    logger     *zap.Logger
    writeMu    sync.Mutex
    closeChan  chan struct{}
    closeOnce  sync.Once
    lastActive time.Time
}

func NewClient(conn *websocket.Conn, logger *zap.Logger) *Client {
    return &Client{
        ID:         uuid.New().String(),
        conn:       conn,
        logger:     logger,
        closeChan:  make(chan struct{}),
        lastActive: time.Now(),
    }
}

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

func (c *Client) Close() {
    c.closeOnce.Do(func() {
        close(c.closeChan)
        c.conn.Close()
    })
}

// 发送 Response
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

// 发送 Request (RPC 调用)
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

// 发送音频流
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

// 启动录音
func (c *Client) StartRecording(config *AudioConfigPayload) error {
    _, err := c.SendRequest(CmdStartRecording, config)
    return err
}

// 停止录音
func (c *Client) StopRecording() error {
    _, err := c.SendRequest(CmdStopRecording, nil)
    return err
}

// 启动播放
func (c *Client) StartPlay(config *AudioConfigPayload) error {
    _, err := c.SendRequest(CmdStartPlay, config)
    return err
}

// 停止播放
func (c *Client) StopPlay() error {
    _, err := c.SendRequest(CmdStopPlay, nil)
    return err
}

func (c *Client) sendJSON(v interface{}) error {
    c.writeMu.Lock()
    defer c.writeMu.Unlock()
    return c.conn.WriteJSON(v)
}
```

### 5. Home Assistant Pipeline 客户端 (internal/homeassistant)

```go
// pipeline.go
package homeassistant

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    
    "github.com/gorilla/websocket"
    "go.uber.org/zap"
)

type PipelineClient struct {
    config     *config.HAConfig
    logger     *zap.Logger
    conn       *websocket.Conn
    msgID      int
    msgMu      sync.Mutex
    
    // 音频流处理
    sttHandlerID int
    audioChan    chan []byte
    resultChan   chan *PipelineResult
}

type PipelineResult struct {
    STTText   string
    TTSAudio  []byte
    Response  string
    Error     error
}

func NewPipelineClient(cfg *config.HAConfig, logger *zap.Logger) *PipelineClient {
    return &PipelineClient{
        config:     cfg,
        logger:     logger,
        audioChan:  make(chan []byte, 100),
        resultChan: make(chan *PipelineResult, 1),
    }
}

// 连接并认证
func (p *PipelineClient) Connect(ctx context.Context) error {
    wsURL := fmt.Sprintf("ws%s/api/websocket", p.config.URL[4:]) // http -> ws
    
    conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
    if err != nil {
        return fmt.Errorf("dial failed: %w", err)
    }
    p.conn = conn
    
    // 等待 auth_required
    var authReq struct {
        Type string `json:"type"`
    }
    if err := conn.ReadJSON(&authReq); err != nil {
        return fmt.Errorf("read auth_required failed: %w", err)
    }
    
    // 发送认证
    auth := map[string]string{
        "type":         "auth",
        "access_token": p.config.Token,
    }
    if err := conn.WriteJSON(auth); err != nil {
        return fmt.Errorf("send auth failed: %w", err)
    }
    
    // 等待 auth_ok
    var authResp struct {
        Type string `json:"type"`
    }
    if err := conn.ReadJSON(&authResp); err != nil {
        return fmt.Errorf("read auth response failed: %w", err)
    }
    
    if authResp.Type != "auth_ok" {
        return fmt.Errorf("auth failed: %s", authResp.Type)
    }
    
    p.logger.Info("Connected to Home Assistant")
    return nil
}

// 运行语音管道
func (p *PipelineClient) RunPipeline(ctx context.Context, audioStream <-chan []byte) (*PipelineResult, error) {
    // 启动 Pipeline
    p.msgMu.Lock()
    p.msgID++
    id := p.msgID
    p.msgMu.Unlock()
    
    pipelineCmd := map[string]interface{}{
        "id":          id,
        "type":        "assist_pipeline/run",
        "start_stage": "stt",
        "end_stage":   "tts",
        "input": map[string]interface{}{
            "sample_rate": 16000,
        },
    }
    
    if err := p.conn.WriteJSON(pipelineCmd); err != nil {
        return nil, fmt.Errorf("send pipeline command failed: %w", err)
    }
    
    result := &PipelineResult{}
    
    // 处理消息
    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }
        
        var msg map[string]interface{}
        if err := p.conn.ReadJSON(&msg); err != nil {
            return nil, fmt.Errorf("read message failed: %w", err)
        }
        
        msgType, _ := msg["type"].(string)
        
        if msgType == "event" {
            event, _ := msg["event"].(map[string]interface{})
            eventType, _ := event["type"].(string)
            eventData, _ := event["data"].(map[string]interface{})
            
            switch eventType {
            case "stt-start":
                // 获取 handler ID 并开始发送音频
                handlerID, _ := eventData["stt_binary_handler_id"].(float64)
                p.sttHandlerID = int(handlerID)
                
                // 启动音频发送协程
                go p.sendAudioStream(audioStream)
                
            case "stt-end":
                // STT 完成
                sttOutput, _ := eventData["stt_output"].(map[string]interface{})
                result.STTText, _ = sttOutput["text"].(string)
                p.logger.Info("STT result", zap.String("text", result.STTText))
                
            case "intent-end":
                // Intent 处理完成
                intentOutput, _ := eventData["intent_output"].(map[string]interface{})
                response, _ := intentOutput["response"].(map[string]interface{})
                speech, _ := response["speech"].(map[string]interface{})
                plain, _ := speech["plain"].(map[string]interface{})
                result.Response, _ = plain["speech"].(string)
                
            case "tts-end":
                // TTS 完成
                ttsOutput, _ := eventData["tts_output"].(map[string]interface{})
                if url, ok := ttsOutput["url"].(string); ok {
                    // 下载 TTS 音频
                    audio, err := p.downloadTTSAudio(url)
                    if err != nil {
                        p.logger.Error("Download TTS audio failed", zap.Error(err))
                    } else {
                        result.TTSAudio = audio
                    }
                }
                return result, nil
                
            case "error":
                errMsg, _ := eventData["message"].(string)
                result.Error = fmt.Errorf("pipeline error: %s", errMsg)
                return result, nil
            }
        }
    }
}

func (p *PipelineClient) sendAudioStream(audioStream <-chan []byte) {
    for audio := range audioStream {
        // 发送格式: [handler_id_byte][audio_data]
        data := append([]byte{byte(p.sttHandlerID)}, audio...)
        if err := p.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
            p.logger.Error("Send audio failed", zap.Error(err))
            return
        }
    }
    
    // 发送结束标记
    p.conn.WriteMessage(websocket.BinaryMessage, []byte{byte(p.sttHandlerID)})
}

func (p *PipelineClient) downloadTTSAudio(url string) ([]byte, error) {
    fullURL := p.config.URL + url
    
    req, _ := http.NewRequest("GET", fullURL, nil)
    req.Header.Set("Authorization", "Bearer "+p.config.Token)
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    return io.ReadAll(resp.Body)
}

func (p *PipelineClient) Close() {
    if p.conn != nil {
        p.conn.Close()
    }
}
```

### 6. 会话管理器 (internal/session)

```go
// manager.go
package session

import (
    "context"
    "sync"
    "time"
    
    "go.uber.org/zap"
)

type SessionManager struct {
    sessions map[string]*Session
    mu       sync.RWMutex
    logger   *zap.Logger
    config   *config.SessionConfig
}

func NewSessionManager(cfg *config.SessionConfig, logger *zap.Logger) *SessionManager {
    return &SessionManager{
        sessions: make(map[string]*Session),
        logger:   logger,
        config:   cfg,
    }
}

func (m *SessionManager) GetOrCreateSession(clientID string) *Session {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if s, exists := m.sessions[clientID]; exists {
        s.Touch()
        return s
    }
    
    s := NewSession(clientID, m.logger)
    m.sessions[clientID] = s
    return s
}

func (m *SessionManager) RemoveSession(clientID string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if s, exists := m.sessions[clientID]; exists {
        s.Close()
        delete(m.sessions, clientID)
    }
}

// 清理过期会话
func (m *SessionManager) CleanupLoop(ctx context.Context) {
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

func (m *SessionManager) cleanup() {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    now := time.Now()
    maxIdle := time.Duration(m.config.MaxIdleTime) * time.Second
    
    for id, s := range m.sessions {
        if now.Sub(s.lastActive) > maxIdle {
            s.Close()
            delete(m.sessions, id)
            m.logger.Info("Session expired", zap.String("id", id))
        }
    }
}
```

```go
// session.go
package session

import (
    "sync"
    "time"
    
    "go.uber.org/zap"
)

type SessionState int

const (
    StateIdle SessionState = iota
    StateListening
    StateProcessing
    StateSpeaking
)

type Session struct {
    ID            string
    State         SessionState
    lastActive    time.Time
    audioChan     chan []byte
    resultChan    chan interface{}
    cancelFunc    context.CancelFunc
    mu            sync.RWMutex
    logger        *zap.Logger
    
    // HA Pipeline 相关
    conversationID string
}

func NewSession(id string, logger *zap.Logger) *Session {
    return &Session{
        ID:         id,
        State:      StateIdle,
        lastActive: time.Now(),
        audioChan:  make(chan []byte, 100),
        resultChan: make(chan interface{}, 1),
        logger:     logger,
    }
}

func (s *Session) Touch() {
    s.mu.Lock()
    s.lastActive = time.Now()
    s.mu.Unlock()
}

func (s *Session) SetState(state SessionState) {
    s.mu.Lock()
    s.State = state
    s.mu.Unlock()
}

func (s *Session) GetState() SessionState {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.State
}

func (s *Session) PushAudio(data []byte) {
    select {
    case s.audioChan <- data:
    default:
        // 缓冲满，丢弃
        s.logger.Warn("Audio buffer full, dropping")
    }
}

func (s *Session) AudioStream() <-chan []byte {
    return s.audioChan
}

func (s *Session) Close() {
    if s.cancelFunc != nil {
        s.cancelFunc()
    }
    close(s.audioChan)
}
```

### 7. 音频处理 (internal/audio)

```go
// vad.go
package audio

import (
    "sync"
    
    silero "github.com/streamer45/silero-vad-go/silero"
)

// VADConfig VAD 配置
type VADConfig struct {
    ModelPath        string  // silero_vad.onnx 模型路径
    SampleRate       int     // 采样率，必须为 16000
    SpeechThreshold  float32 // 语音概率阈值 (0-1)，推荐 0.5
    SilenceTimeoutMs int     // 静音超时（毫秒）
    MinSpeechMs      int     // 最小语音时长（毫秒）
}

// DefaultVADConfig 返回默认配置
func DefaultVADConfig() VADConfig {
    return VADConfig{
        ModelPath:        "models/silero_vad.onnx",
        SampleRate:       16000,
        SpeechThreshold:  0.5,
        SilenceTimeoutMs: 1200,
        MinSpeechMs:      100,
    }
}

// VAD 语音活动检测器（基于 Silero VAD）
type VAD struct {
    detector *silero.Detector
    config   VADConfig
    mu       sync.Mutex

    // 状态追踪
    isSpeaking       bool
    silenceFrames    int
    speechFrames     int
    maxSilenceFrames int
    minSpeechFrames  int
}

// NewVAD 创建 VAD 实例
func NewVAD(cfg VADConfig) (*VAD, error) {
    detector, err := silero.NewDetector(silero.DetectorConfig{
        ModelPath:       cfg.ModelPath,
        SampleRate:      cfg.SampleRate,
        Threshold:       cfg.SpeechThreshold,
        MinSilenceDurationMs: cfg.SilenceTimeoutMs,
        SpeechPadMs:     30,
    })
    if err != nil {
        return nil, err
    }

    frameDurationMs := 30 // Silero 推荐 30ms 帧
    return &VAD{
        detector:         detector,
        config:           cfg,
        maxSilenceFrames: cfg.SilenceTimeoutMs / frameDurationMs,
        minSpeechFrames:  cfg.MinSpeechMs / frameDurationMs,
    }, nil
}

type VADResult int

const (
    VADResultSilence VADResult = iota
    VADResultSpeech
    VADResultSpeechEnd
)

// Process 处理一帧音频（30ms @ 16kHz = 480 samples = 960 bytes）
func (v *VAD) Process(frame []byte) VADResult {
    v.mu.Lock()
    defer v.mu.Unlock()

    // 转换为 float32 样本
    samples := bytesToFloat32(frame)
    
    // Silero VAD 检测，返回语音概率 (0-1)
    segments, err := v.detector.Detect(samples)
    if err != nil {
        return VADResultSilence
    }

    // 判断是否检测到语音
    isSpeech := len(segments) > 0

    if isSpeech {
        v.speechFrames++
        v.silenceFrames = 0

        if !v.isSpeaking && v.speechFrames >= v.minSpeechFrames {
            v.isSpeaking = true
        }

        if v.isSpeaking {
            return VADResultSpeech
        }
        return VADResultSilence
    }

    // 静音帧
    v.speechFrames = 0

    if v.isSpeaking {
        v.silenceFrames++
        if v.silenceFrames >= v.maxSilenceFrames {
            v.isSpeaking = false
            v.silenceFrames = 0
            return VADResultSpeechEnd
        }
        return VADResultSpeech // 短暂静音仍算说话
    }

    return VADResultSilence
}

// bytesToFloat32 将 16-bit PCM 转换为 float32
func bytesToFloat32(data []byte) []float32 {
    samples := make([]float32, len(data)/2)
    for i := 0; i < len(data)-1; i += 2 {
        sample := int16(data[i]) | int16(data[i+1])<<8
        samples[i/2] = float32(sample) / 32768.0
    }
    return samples
}

func (v *VAD) Reset() {
    v.mu.Lock()
    defer v.mu.Unlock()
    v.isSpeaking = false
    v.silenceFrames = 0
    v.speechFrames = 0
    v.detector.Reset()
}

func (v *VAD) Close() {
    v.detector.Destroy()
}
```

### 8. 主程序入口 (cmd/bridge)

```go
// main.go
package main

import (
    "context"
    "flag"
    "os"
    "os/signal"
    "syscall"
    
    "go.uber.org/zap"
    
    "github.com/user/open-xiaoai-ha-bridge/internal/config"
    "github.com/user/open-xiaoai-ha-bridge/internal/xiaoai"
    "github.com/user/open-xiaoai-ha-bridge/internal/homeassistant"
    "github.com/user/open-xiaoai-ha-bridge/internal/session"
)

func main() {
    configPath := flag.String("config", "config.yaml", "config file path")
    flag.Parse()
    
    // 初始化日志
    logger, _ := zap.NewProduction()
    defer logger.Sync()
    
    // 加载配置
    cfg, err := config.Load(*configPath)
    if err != nil {
        logger.Fatal("Failed to load config", zap.Error(err))
    }
    
    // 创建上下文
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // 初始化组件
    sessionMgr := session.NewSessionManager(&cfg.Session, logger)
    go sessionMgr.CleanupLoop(ctx)
    
    // 创建 Bridge
    bridge := NewBridge(cfg, sessionMgr, logger)
    
    // 启动 XiaoAI WebSocket 服务器
    xiaoaiServer := xiaoai.NewServer(&cfg.Server, logger)
    
    // 注册事件处理
    xiaoaiServer.OnClientConnect(func(client *xiaoai.Client) {
        logger.Info("Client connected", zap.String("id", client.ID))
        sessionMgr.GetOrCreateSession(client.ID)
    })
    
    xiaoaiServer.OnClientDisconnect(func(client *xiaoai.Client) {
        logger.Info("Client disconnected", zap.String("id", client.ID))
        sessionMgr.RemoveSession(client.ID)
    })
    
    xiaoaiServer.OnEvent(func(client *xiaoai.Client, event *xiaoai.Event) {
        bridge.HandleEvent(ctx, client, event)
    })
    
    xiaoaiServer.OnStream(func(client *xiaoai.Client, stream *xiaoai.Stream) {
        bridge.HandleStream(ctx, client, stream)
    })
    
    // 启动服务器
    go func() {
        if err := xiaoaiServer.Start(ctx); err != nil {
            logger.Error("Server error", zap.Error(err))
        }
    }()
    
    logger.Info("Bridge started", 
        zap.String("listen", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)),
        zap.String("ha_url", cfg.HomeAssistant.URL))
    
    // 等待退出信号
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan
    
    logger.Info("Shutting down...")
    cancel()
}
```

### 9. Bridge 核心逻辑

```go
// bridge.go
package main

import (
    "context"
    "encoding/json"
    
    "go.uber.org/zap"
    
    "github.com/user/open-xiaoai-ha-bridge/internal/xiaoai"
    "github.com/user/open-xiaoai-ha-bridge/internal/homeassistant"
    "github.com/user/open-xiaoai-ha-bridge/internal/session"
    "github.com/user/open-xiaoai-ha-bridge/internal/audio"
    "github.com/user/open-xiaoai-ha-bridge/internal/config"
)

type Bridge struct {
    config     *config.Config
    sessionMgr *session.SessionManager
    logger     *zap.Logger
}

func NewBridge(cfg *config.Config, sessionMgr *session.SessionManager, logger *zap.Logger) *Bridge {
    return &Bridge{
        config:     cfg,
        sessionMgr: sessionMgr,
        logger:     logger,
    }
}

func (b *Bridge) HandleEvent(ctx context.Context, client *xiaoai.Client, event *xiaoai.Event) {
    sess := b.sessionMgr.GetOrCreateSession(client.ID)
    
    switch event.Event {
    case xiaoai.EventKWS:
        b.handleKWSEvent(ctx, client, sess, event)
    case xiaoai.EventInstruction:
        b.handleInstructionEvent(ctx, client, sess, event)
    case xiaoai.EventPlaying:
        b.handlePlayingEvent(ctx, client, sess, event)
    }
}

func (b *Bridge) handleKWSEvent(ctx context.Context, client *xiaoai.Client, sess *session.Session, event *xiaoai.Event) {
    var data xiaoai.KWSEventData
    if err := json.Unmarshal(event.Data, &data); err != nil {
        b.logger.Error("Parse KWS event failed", zap.Error(err))
        return
    }
    
    if data.Keyword != "" {
        b.logger.Info("Wake word detected", zap.String("keyword", data.Keyword))
        
        // 开始语音交互流程
        go b.startVoiceInteraction(ctx, client, sess)
    }
}

func (b *Bridge) startVoiceInteraction(ctx context.Context, client *xiaoai.Client, sess *session.Session) {
    // 1. 设置状态为监听中
    sess.SetState(session.StateListening)
    
    // 2. 启动录音
    audioConfig := &xiaoai.AudioConfigPayload{
        Channels:      1,
        BitsPerSample: 16,
        SampleRate:    16000,
        BufferSize:    960,  // 30ms @ 16kHz
        PeriodSize:    480,
    }
    
    if err := client.StartRecording(audioConfig); err != nil {
        b.logger.Error("Start recording failed", zap.Error(err))
        return
    }
    
    // 3. 创建 HA Pipeline 客户端
    haClient := homeassistant.NewPipelineClient(&b.config.HomeAssistant, b.logger)
    if err := haClient.Connect(ctx); err != nil {
        b.logger.Error("Connect to HA failed", zap.Error(err))
        client.StopRecording()
        return
    }
    defer haClient.Close()
    
    // 4. 创建带取消的上下文
    pipelineCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    
    // 5. 运行 Pipeline（流式处理）
    result, err := haClient.RunPipeline(pipelineCtx, sess.AudioStream())
    
    // 6. 停止录音
    client.StopRecording()
    
    if err != nil {
        b.logger.Error("Pipeline failed", zap.Error(err))
        return
    }
    
    b.logger.Info("Pipeline completed",
        zap.String("stt", result.STTText),
        zap.String("response", result.Response))
    
    // 7. 播放 TTS 响应
    if len(result.TTSAudio) > 0 {
        sess.SetState(session.StateSpeaking)
        
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
        
        // 发送音频数据
        b.streamAudioToClient(client, result.TTSAudio)
        
        client.StopPlay()
    }
    
    sess.SetState(session.StateIdle)
}

func (b *Bridge) HandleStream(ctx context.Context, client *xiaoai.Client, stream *xiaoai.Stream) {
    sess := b.sessionMgr.GetOrCreateSession(client.ID)
    
    if stream.Tag == "record" && sess.GetState() == session.StateListening {
        // 推送音频到会话
        sess.PushAudio(stream.Bytes)
    }
}

func (b *Bridge) streamAudioToClient(client *xiaoai.Client, audio []byte) {
    // 分块发送音频
    chunkSize := 4800 // 100ms @ 24kHz, 16bit
    
    for i := 0; i < len(audio); i += chunkSize {
        end := i + chunkSize
        if end > len(audio) {
            end = len(audio)
        }
        
        if err := client.SendStream("play", audio[i:end]); err != nil {
            b.logger.Error("Send audio failed", zap.Error(err))
            return
        }
        
        // 控制发送速率，避免缓冲溢出
        time.Sleep(80 * time.Millisecond)
    }
}

func (b *Bridge) handleInstructionEvent(ctx context.Context, client *xiaoai.Client, sess *session.Session, event *xiaoai.Event) {
    // 处理来自小爱原生 STT 的指令（备用）
    b.logger.Debug("Instruction event", zap.Any("data", event.Data))
}

func (b *Bridge) handlePlayingEvent(ctx context.Context, client *xiaoai.Client, sess *session.Session, event *xiaoai.Event) {
    // 更新播放状态
    b.logger.Debug("Playing event", zap.Any("data", event.Data))
}
```

---

## 配置文件模板

```yaml
# config.yaml
server:
  host: "0.0.0.0"
  port: 4399

homeassistant:
  url: "http://192.168.1.100:8123"
  token: "YOUR_LONG_LIVED_ACCESS_TOKEN"
  # pipeline_id: ""  # 可选，使用默认 Pipeline

audio:
  input_sample_rate: 16000
  output_sample_rate: 24000
  channels: 1
  bits_per_sample: 16
  vad_enabled: true
  vad_threshold: 3
  silence_timeout: 1000  # ms

session:
  max_idle_time: 300      # 秒
  max_session_time: 3600  # 秒

log:
  level: "info"
  format: "json"
```

---

## 依赖管理

```go
// go.mod
module github.com/user/open-xiaoai-ha-bridge

go 1.22

require (
    github.com/gorilla/websocket v1.5.1
    github.com/google/uuid v1.6.0
    github.com/nicerloop/gowebrtcvad v0.0.0-20210807124013-9f8aa7e5e2f7
    go.uber.org/zap v1.26.0
    gopkg.in/yaml.v3 v3.0.1
)
```

---

## Docker 部署

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app

# 安装依赖
RUN apk add --no-cache git gcc musl-dev

# 复制 go.mod/sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 构建
RUN CGO_ENABLED=1 go build -o bridge ./cmd/bridge

# 运行镜像
FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/bridge .
COPY configs/config.yaml .

EXPOSE 4399

CMD ["./bridge", "-config", "config.yaml"]
```

```yaml
# docker-compose.yaml
version: '3.8'

services:
  ha-bridge:
    build: .
    container_name: xiaoai-ha-bridge
    ports:
      - "4399:4399"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
    environment:
      - TZ=Asia/Shanghai
    restart: unless-stopped
    networks:
      - ha-network

networks:
  ha-network:
    external: true
```

---

## 开发与测试

### 本地运行

```bash
# 安装依赖
go mod tidy

# 运行
go run ./cmd/bridge -config config.yaml

# 构建
go build -o bridge ./cmd/bridge
```

### 集成测试

```go
// internal/xiaoai/client_test.go
package xiaoai_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestProtocolParsing(t *testing.T) {
    // 测试协议解析
    jsonData := `{"Event":{"id":"123","event":"kws","data":{"Keyword":"你好小智"}}}`
    
    var msg AppMessage
    err := json.Unmarshal([]byte(jsonData), &msg)
    
    assert.NoError(t, err)
    assert.NotNil(t, msg.Event)
    assert.Equal(t, "kws", msg.Event.Event)
}
```

---

## 实施计划

### 第一阶段：基础框架（1 周）

- [ ] 项目初始化，目录结构
- [ ] 配置管理模块
- [ ] 日志模块
- [ ] Open-XiaoAI 协议定义
- [ ] WebSocket 服务器基础实现

### 第二阶段：核心功能（2 周）

- [ ] 客户端连接管理
- [ ] 会话管理器
- [ ] HA WebSocket 客户端
- [ ] Assist Pipeline 集成
- [ ] 音频流转发

### 第三阶段：优化与测试（1 周）

- [ ] VAD 集成
- [ ] 音频格式转换
- [ ] 错误处理与重连
- [ ] 单元测试
- [ ] 集成测试

### 第四阶段：部署与文档（0.5 周）

- [ ] Docker 镜像构建
- [ ] 部署文档
- [ ] 使用说明

---

## 延时分析

| 阶段 | 预计延时 | 优化措施 |
|------|----------|----------|
| 唤醒词检测 | ~200ms | 小爱原生 KWS（硬件 DSP 加速） |
| 接管 + 提示音 | 50-100ms | abort_xiaoai() + 可选提示音 |
| 网络传输 | 10-50ms | 局域网 WebSocket |
| VAD + 录音 | 并行 | Bridge 端 WebRTC VAD |
| STT 处理 | 300-800ms | HA Faster-Whisper |
| Intent 处理 | 50-100ms | HA 本地 Intent 匹配 |
| TTS 生成 | 100-300ms | HA Piper 流式 |
| **总计** | **710-1550ms** | |

---

## 修订记录

| 版本 | 日期 | 修改内容 |
|------|------|----------|
| v1.0 | 2026-01-08 | 初始技术方案 |
| v1.1 | 2026-01-08 | 新增技术评估章节：分析小爱原生 KWS/VAD 机制，确定使用小爱原生唤醒词方案，放弃 Client 端 Sherpa-ONNX |
| v1.2 | 2026-01-08 | 新增多轮对话设计章节：会话状态机、超时等待、退出提醒、HA 上下文保持 |
| v1.3 | 2026-01-09 | VAD 方案从 WebRTC VAD 升级为 Silero VAD (ONNX)，更新代码示例 |
| v1.4 | 2026-01-09 | 新增实现状态更新章节，记录已完成功能与优化项 |

---

## 实现状态更新 (2026-01-09)

> 此章节记录设计文档与实际实现的对比，保留历史设计思路，补充实现细节。

### 实施计划完成状态

#### 第一阶段：基础框架 ✅ 100%

- [x] 项目初始化，目录结构
- [x] 配置管理模块 (`internal/config/config.go`)
- [x] 日志模块 (`pkg/logger/logger.go`)
- [x] Open-XiaoAI 协议定义 (`internal/xiaoai/protocol.go`)
- [x] WebSocket 服务器基础实现 (`internal/xiaoai/server.go`)

#### 第二阶段：核心功能 ✅ 100%

- [x] 客户端连接管理 (`internal/xiaoai/client.go`)
- [x] 会话管理器 (`internal/session/manager.go`, `session.go`, `state.go`)
- [x] HA WebSocket 客户端 (`internal/homeassistant/client.go`)
- [x] Assist Pipeline 集成 (`internal/homeassistant/pipeline.go`)
- [x] 音频流转发 (`cmd/bridge/bridge.go`)

#### 第三阶段：优化与测试 ⏳ 70%

- [x] VAD 集成 (已完成 Silero VAD)
- [ ] 音频格式转换 (未实现独立 `resample.go`，TTS 直接播放)
- [x] 错误处理与重连 (基础实现)
- [ ] 单元测试 (待完成)
- [ ] 集成测试 (待完成)

#### 第四阶段：部署与文档 ✅ 100%

- [x] Docker 镜像构建 (`Dockerfile`)
- [x] 部署文档 (`README.md`)
- [x] 使用说明

---

### VAD 方案优化

> 原设计使用 WebRTC VAD，实际实现升级为 Silero VAD

#### 原方案 (WebRTC VAD)

```go
// 原设计
type VADConfig struct {
    Aggressiveness int  // 0-3, 3 最激进
    SilenceTimeout int
}
```

#### 实际实现 (Silero VAD + 双阈值 + 滑动窗口)

参考 [xiaozhi-esp32-server](https://github.com/xinnan-tech/xiaozhi-esp32-server) 优化：

```go
// 实际实现
type VADConfig struct {
    ModelPath          string  // silero_vad.onnx
    SpeechThreshold    float32 // 高阈值 (0.5)
    SpeechThresholdLow float32 // 低阈值 (0.2) - 避免边界抖动
    SilenceTimeoutMs   int
    WindowSize         int     // 滑动窗口 (5)
    WindowThreshold    int     // 窗口阈值 (3)
}
```

**优化点**：

| 特性 | 原设计 | 实际实现 |
|------|--------|----------|
| VAD 库 | WebRTC VAD (GMM) | Silero VAD (DNN) |
| 准确率 | ~50% | ~87.7% |
| 双阈值 | ❌ | ✅ 避免边界抖动 |
| 滑动窗口 | ❌ | ✅ 过滤瞬时噪声 |
| 时间计算 | 帧数估算 | ✅ 精确时间戳 |

---

### 项目文件结构对比

#### 设计文档 vs 实际实现

```diff
open-xiaoai-ha-bridge/
├── cmd/
│   └── bridge/
│       ├── main.go            # ✅ 已实现
+       └── bridge.go          # [新增] 核心 Bridge 逻辑
├── internal/
│   ├── config/
│   │   └── config.go          # ✅ 已实现
│   ├── xiaoai/
│   │   ├── server.go          # ✅ 已实现
│   │   ├── client.go          # ✅ 已实现
│   │   ├── protocol.go        # ✅ 已实现
-   │   └── handler.go         # [未实现] 逻辑合并到 bridge.go
│   ├── homeassistant/
│   │   ├── client.go          # ✅ 已实现
│   │   ├── pipeline.go        # ✅ 已实现
-   │   ├── conversation.go    # [未实现] API 合并到 pipeline.go
│   │   └── types.go           # ✅ 已实现
│   ├── session/
│   │   ├── manager.go         # ✅ 已实现
│   │   ├── session.go         # ✅ 已实现
│   │   └── state.go           # ✅ 已实现
│   ├── audio/
│   │   ├── buffer.go          # ✅ 已实现
│   │   ├── vad.go             # ✅ 已实现 (Silero VAD)
-   │   └── resample.go        # [未实现] 暂不需要
-   └── router/
-       └── router.go          # [未实现] 逻辑合并到 server.go
├── pkg/
-   ├── websocket/
-   │   └── websocket.go       # [未实现] 使用 gorilla/websocket
│   └── logger/
│       └── logger.go          # ✅ 已实现
├── configs/
│   └── config.yaml            # ✅ 已实现
+├── scripts/
+│   └── build.sh              # [新增] 构建脚本
+├── third_party/
+│   └── onnxruntime/          # [新增] ONNX Runtime
+├── models/
+│   └── silero_vad.onnx       # [新增] VAD 模型
├── Dockerfile                 # ✅ 已实现 (含 ONNX Runtime)
├── docker-compose.yaml        # ✅ 已实现
├── go.mod                     # ✅ 已实现
└── README.md                  # ✅ 已实现
```

---

### 依赖变更

| 设计文档 | 实际实现 |
|----------|----------|
| `github.com/nicerloop/gowebrtcvad` | `github.com/streamer45/silero-vad-go` |
| 无 ONNX 依赖 | ONNX Runtime 1.16.3 (~17MB) |
| 无模型文件 | `silero_vad.onnx` (~290KB) |

---

### 构建方式变更

原设计文档中的构建命令：

```bash
go build -o bridge ./cmd/bridge
```

实际需要设置 CGO 环境变量：

```bash
# 方式 1: 手动设置
CGO_CFLAGS="-I$(pwd)/third_party/onnxruntime/include" \
CGO_LDFLAGS="-L$(pwd)/third_party/onnxruntime/lib" \
go build -o ha-bridge ./cmd/bridge

# 方式 2: 使用构建脚本
./scripts/build.sh build
```

---

### 待完成项

1. **单元测试** - 协议解析、会话管理、VAD 逻辑
2. **集成测试** - 端到端语音交互测试
3. **音频重采样** - 如需支持不同采样率的 TTS 输出
4. **HA 重连机制** - WebSocket 断线自动重连
5. **健康检查端点** - HTTP `/health` API

---

*技术方案版本：v1.4*
*最后更新：2026-01-09*

