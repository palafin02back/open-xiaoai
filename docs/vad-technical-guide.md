# VAD 语音活动检测技术科普

> 本文档作为 Open-XiaoAI HA Bridge 项目的技术科普补充，详细介绍 VAD（Voice Activity Detection）的工作原理。

---

## 目录

1. [什么是 VAD](#什么是-vad)
2. [为什么需要 VAD](#为什么需要-vad)
3. [人声的物理特征](#人声的物理特征)
4. [VAD 如何识别人声](#vad-如何识别人声)
5. [常见 VAD 算法](#常见-vad-算法)
6. [WebRTC VAD 详解](#webrtc-vad-详解)
7. [VAD 调优与最佳实践](#vad-调优与最佳实践)
8. [代码示例](#代码示例)

---

## 什么是 VAD

**VAD（Voice Activity Detection）**，中文称为"语音活动检测"或"语音端点检测"，是一种判断音频信号中是否包含人类语音的技术。

```
输入音频流:
┌────────────────────────────────────────────────────────────────┐
│ [静音] [噪声] [说话说话说话] [噪声] [静音] [说话说话] [静音]   │
└────────────────────────────────────────────────────────────────┘
                    ↓  VAD 处理
输出结果:
┌────────────────────────────────────────────────────────────────┐
│ [  0 ] [  0 ] [    1  1  1   ] [  0 ] [  0 ] [  1  1  ] [  0 ] │
└────────────────────────────────────────────────────────────────┘
                ↑ 检测到语音区域        ↑ 检测到语音区域
```

**核心功能**：
- 判断"现在有人在说话吗？"
- 定位语音的起始点和结束点
- 区分语音和非语音（噪声、静音、音乐等）

---

## 为什么需要 VAD

### 1. 节省计算资源

```
无 VAD:
[全部音频] ──────────────────────────────► [STT 引擎处理全部]
                                              ↑
                                         资源浪费严重

有 VAD:
[全部音频] ──► [VAD 筛选] ──► [只发送语音部分] ──► [STT 引擎]
                  ↓                                    ↑
            丢弃无声部分                          节省 70%+ 资源
```

### 2. 降低网络传输

| 场景 | 无 VAD | 有 VAD | 节省 |
|------|--------|--------|------|
| 10秒对话（说话3秒）| 160KB | 48KB | 70% |
| 持续监听1分钟 | 960KB | 约200KB | 79% |

### 3. 提高识别准确率

VAD 过滤掉噪声后，STT 引擎只处理干净的语音，**减少误识别**。

### 4. 实现对话交互

```
VAD 检测到语音开始 ──► 系统开始录音
VAD 检测到语音结束 ──► 系统停止录音，发送给 STT
```

---

## 人声的物理特征

要理解 VAD 如何工作，需要先了解人声与其他声音的区别。

### 1. 人声的产生

```
┌─────────────────────────────────────────────────────────────────┐
│                      人声发声原理                                │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  肺部气流 ──► 声带振动 ──► 声道共鸣 ──► 嘴唇/鼻腔 ──► 声波输出  │
│              ↓           ↓                                      │
│          产生基频       产生共振峰                               │
│          (F0)          (F1, F2, F3)                             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 2. 人声的频率特征

#### 基频 (Fundamental Frequency, F0)

声带振动产生的最低频率，决定音高。

| 人群 | 基频范围 | 典型值 |
|------|----------|--------|
| 成年男性 | 85-180 Hz | 120 Hz |
| 成年女性 | 165-255 Hz | 200 Hz |
| 儿童 | 250-500 Hz | 300 Hz |

#### 谐波 (Harmonics)

基频的整数倍频率，形成人声特有的"音色"。

```
人声频谱 (典型):

振幅 │
     │   ┌─┐
     │   │ │     ┌─┐
     │   │ │     │ │     ┌─┐
     │   │ │     │ │     │ │     ┌─┐
     │   │ │     │ │     │ │     │ │     ┌─┐
     └───┴─┴─────┴─┴─────┴─┴─────┴─┴─────┴─┴─────► 频率
         F0      2F0     3F0     4F0     5F0
        基频     谐波    谐波    谐波    谐波
        120Hz   240Hz   360Hz   480Hz   600Hz
```

#### 共振峰 (Formants)

声道共鸣形成的频率峰值，区分不同的元音。

| 共振峰 | 频率范围 | 决定因素 |
|--------|----------|----------|
| F1 | 300-900 Hz | 开口度（嘴张开程度）|
| F2 | 900-2200 Hz | 舌位前后 |
| F3 | 2200-3000 Hz | 口腔形状 |

```
不同元音的 F1/F2 分布:

F2 (Hz)
2500 │    "i"(衣)
     │    ·
2000 │         "e"(诶)
     │         ·
1500 │
     │              "a"(啊)
1000 │              ·
     │
 500 │ "u"(乌)
     │  ·
     └─────────────────────────► F1 (Hz)
       200   400   600   800
```

### 3. 人声 vs 噪声的频谱对比

```
人声频谱 (有规律的谐波结构):
振幅 │  ∧
     │ ╱ ╲    ∧
     │╱   ╲  ╱ ╲   ∧
     │     ╲╱   ╲ ╱ ╲   ∧
     │          ╲╱   ╲ ╱ ╲
     └────────────────────────► 频率
       ↑ 基频    ↑ 谐波们（等间距）


白噪声频谱 (无规律，全频段均匀):
振幅 │╱╲ ╱╲ ╱╲╱╲ ╱╲╱╲ ╱╲
     │  ╲╱  ╲╱    ╲╱  ╲╱  ╲
     └────────────────────────► 频率
       ↑ 随机分布，无明显峰值


空调/风扇噪声 (低频为主):
振幅 │╲
     │ ╲
     │  ╲╲
     │    ╲╲__________
     └────────────────────────► 频率
       ↑ 低频能量集中
```

### 4. 时域特征对比

| 特征 | 人声 | 白噪声 | 环境噪声 |
|------|------|--------|----------|
| **能量变化** | 有规律起伏（音节节奏）| 相对稳定 | 缓慢变化 |
| **过零率** | 中等（浊音）/ 高（清音）| 高且随机 | 低 |
| **持续时间** | 音节 100-300ms | 持续 | 持续 |
| **包络形状** | 有起音、持续、收音 | 无明显变化 | 无明显变化 |

```
人声波形 (说"你好"):

振幅
     │      ∧∧∧∧           ∧∧∧∧∧
     │     ∧    ∧         ∧     ∧
     │    ∧      ∧       ∧       ∧
  0  │───/        \─────/         \─────
     │  ▲          ▲   ▲           ▲
     │  起音      收音  起音       收音
     └────────────────────────────────► 时间
        "你"           "好"


噪声波形 (随机):

振幅
     │╱╲╱╲/\╱\╱╲/╲/\╱\╲/╱╲╱╲/\╱\╱╲/╱╲
  0  │─────────────────────────────────
     │╲╱╲╱\/╲/╲╱\╱\/╲/╱╲╱╲/\/╲/╲╱\╱╲/
     └────────────────────────────────► 时间
        （无规律变化）
```

---

## VAD 如何识别人声

VAD 通过提取和分析音频特征来判断是否为人声。

### 1. 核心特征

#### 短时能量 (Short-Time Energy)

```python
# 计算公式
energy = sum(sample ** 2 for sample in frame) / len(frame)
```

| 情况 | 能量水平 | 判断 |
|------|----------|------|
| 静音 | 很低 | 非语音 |
| 轻声说话 | 中等 | 可能是语音 |
| 正常说话 | 较高 | 可能是语音 |
| 噪声 | 稳定中等 | 需结合其他特征 |

#### 过零率 (Zero Crossing Rate)

信号在一帧内穿过零点的次数，反映频率特性。

```python
# 计算公式
zcr = sum(1 for i in range(1, len(frame)) 
          if frame[i] * frame[i-1] < 0) / len(frame)
```

| 信号类型 | 过零率 | 原因 |
|----------|--------|------|
| 浊音（元音）| 低-中 | 周期性强 |
| 清音（s, f）| 高 | 类似噪声 |
| 白噪声 | 很高且随机 | 无周期性 |

#### 频谱熵 (Spectral Entropy)

衡量频谱的"混乱程度"。

```python
# 计算公式
spectral_entropy = -sum(p * log(p) for p in normalized_spectrum)
```

| 信号类型 | 频谱熵 | 原因 |
|----------|--------|------|
| 人声 | 低 | 能量集中在谐波上 |
| 噪声 | 高 | 能量均匀分布 |

#### 频谱平坦度 (Spectral Flatness)

```python
# 计算公式
flatness = geometric_mean(spectrum) / arithmetic_mean(spectrum)
```

| 信号类型 | 平坦度 | 说明 |
|----------|--------|------|
| 人声 | 接近 0 | 有明显峰值（谐波）|
| 白噪声 | 接近 1 | 频谱平坦 |

### 2. 判决逻辑

简单的基于阈值的 VAD：

```
IF 能量 > 能量阈值 AND
   频谱熵 < 熵阈值 AND
   (过零率 在合理范围内) THEN
    判定为语音
ELSE
    判定为非语音
```

更先进的基于模型的 VAD：

```
语音概率 = P(语音 | 特征向量)
噪声概率 = P(噪声 | 特征向量)

IF 语音概率 / 噪声概率 > 阈值 THEN
    判定为语音
```

---

## 常见 VAD 算法

### 1. 算法分类

| 类别 | 方法 | 优点 | 缺点 |
|------|------|------|------|
| **基于能量** | 短时能量阈值 | 简单快速 | 易受噪声影响 |
| **基于频谱** | 频谱熵、平坦度 | 噪声鲁棒性好 | 计算量较大 |
| **基于模型** | GMM、HMM | 准确率高 | 需要训练数据 |
| **基于深度学习** | RNN、CNN | 最高准确率 | 计算资源需求高 |

### 2. 常用开源实现

| 名称 | 技术 | 语言 | 特点 |
|------|------|------|------|
| **WebRTC VAD** | GMM | C/多语言绑定 | 极轻量、低延时 |
| **Silero VAD** | ONNX 神经网络 | Python/多语言 | 高准确率 |
| **py-webrtcvad** | WebRTC 绑定 | Python | 易用 |
| **libfvad** | 改进版 WebRTC | C | 性能优化 |

---

## WebRTC VAD 详解

WebRTC VAD 是 Google 为 WebRTC 项目开发的轻量级 VAD，被广泛应用于各类语音应用。

### 1. 工作原理

```
┌─────────────────────────────────────────────────────────────────┐
│                   WebRTC VAD 处理流程                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  音频帧 ──► 分帧 ──► FFT ──► 子带能量 ──► GMM分类 ──► 0/1     │
│  (10/20/30ms)        ↓                                          │
│                      ↓                                          │
│            ┌─────────────────────────────────┐                  │
│            │       6 个子带能量提取          │                  │
│            │  ┌───────────────────────────┐  │                  │
│            │  │ Band 0: 80-250 Hz         │  │                  │
│            │  │ Band 1: 250-500 Hz        │  │                  │
│            │  │ Band 2: 500-1000 Hz       │  │                  │
│            │  │ Band 3: 1000-2000 Hz      │  │                  │
│            │  │ Band 4: 2000-3000 Hz      │  │                  │
│            │  │ Band 5: 3000-4000 Hz      │  │                  │
│            │  └───────────────────────────┘  │                  │
│            └─────────────────────────────────┘                  │
│                          ↓                                      │
│            ┌─────────────────────────────────┐                  │
│            │     高斯混合模型 (GMM) 分类     │                  │
│            │  ┌───────────────────────────┐  │                  │
│            │  │ 语音模型: 学习语音的子带   │  │                  │
│            │  │          能量分布特征      │  │                  │
│            │  ├───────────────────────────┤  │                  │
│            │  │ 噪声模型: 自适应学习当前   │  │                  │
│            │  │          环境的噪声分布    │  │                  │
│            │  └───────────────────────────┘  │                  │
│            │                                 │                  │
│            │  判决: P(语音)/P(噪声) > 阈值?  │                  │
│            └─────────────────────────────────┘                  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 2. Aggressiveness 参数

| 级别 | 说明 | 适用场景 |
|------|------|----------|
| **0** | 最宽松 | 安静环境，需要捕获所有语音 |
| **1** | 较宽松 | 轻度噪声环境 |
| **2** | 较严格 | 中度噪声环境（推荐）|
| **3** | 最严格 | 高噪声环境，可能丢失部分语音 |

```
Aggressiveness = 0 (宽松):
输入: [静音][轻噪声][说话][重噪声][说话]
输出: [  0 ][  1   ][ 1  ][  1   ][ 1  ]
                ↑ 噪声也被识别为语音

Aggressiveness = 3 (严格):
输入: [静音][轻噪声][说话][重噪声][说话]
输出: [  0 ][  0   ][ 1  ][  0   ][ 1  ]
                          ↑ 噪声被过滤
```

### 3. 帧长度要求

WebRTC VAD 只支持特定的帧长度：

| 采样率 | 支持的帧长度 |
|--------|--------------|
| 8000 Hz | 10ms, 20ms, 30ms |
| 16000 Hz | 10ms, 20ms, 30ms |
| 32000 Hz | 10ms, 20ms, 30ms |
| 48000 Hz | 10ms, 20ms, 30ms |

```
16000 Hz 采样率下:
- 10ms = 160 samples = 320 bytes
- 20ms = 320 samples = 640 bytes
- 30ms = 480 samples = 960 bytes (推荐)
```

---

## VAD 调优与最佳实践

### 1. 常见问题与解决

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 噪声被误识别为语音 | Aggressiveness 太低 | 提高到 2 或 3 |
| 语音开头被截断 | VAD 反应慢 | 保留语音前的缓冲帧 |
| 语音被中途切断 | 静音阈值太短 | 增加静音超时时间 |
| 轻声说话检测不到 | 能量阈值过高 | 降低 Aggressiveness |

### 2. 语音开始/结束检测

为避免误触发和漏检，需要加入状态机：

```
┌─────────────────────────────────────────────────────────────────┐
│                    VAD 状态机                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌─────────┐  连续N帧语音   ┌─────────┐                        │
│  │  静音   │ ────────────► │ 说话中  │                        │
│  │ (IDLE)  │               │(SPEECH) │                        │
│  └────┬────┘               └────┬────┘                        │
│       ↑                         │                              │
│       │    连续M帧静音          │                              │
│       └─────────────────────────┘                              │
│                                                                 │
│  推荐参数:                                                      │
│  N = 3 帧 (约 90ms) - 确认说话开始                             │
│  M = 30-50 帧 (约 1-1.5s) - 确认说话结束                       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 3. 预缓冲技术

```
问题: VAD 检测到语音时，可能已经丢失了前几帧

解决: 始终保留最近 N 帧的缓冲

┌────────────────────────────────────────────────────────────────┐
│ [缓冲1][缓冲2][缓冲3]│ VAD开始检测到语音 │ 后续语音...        │
│        ↑             ↑                                         │
│   这些帧也要发送   从这里开始检测到                            │
└────────────────────────────────────────────────────────────────┘

推荐: 保留 5-10 帧 (150-300ms) 的预缓冲
```

### 4. 针对不同场景的配置

```yaml
# 安静室内环境
aggressiveness: 1
silence_timeout_ms: 1500
pre_buffer_frames: 5

# 有背景音乐/电视
aggressiveness: 3
silence_timeout_ms: 1000
pre_buffer_frames: 10

# 户外/嘈杂环境
aggressiveness: 3
silence_timeout_ms: 800
pre_buffer_frames: 10
min_speech_frames: 5  # 需要更多帧确认
```

---

## 代码示例

### Go 语言实现

```go
package vad

import (
    "sync"
    
    webrtcvad "github.com/nicerloop/gowebrtcvad"
)

// VADConfig VAD 配置
type VADConfig struct {
    SampleRate       int // 采样率: 8000, 16000, 32000, 48000
    FrameDurationMs  int // 帧长度: 10, 20, 30
    Aggressiveness   int // 激进程度: 0-3
    SilenceTimeoutMs int // 静音超时（毫秒）
    PreBufferFrames  int // 预缓冲帧数
    MinSpeechFrames  int // 最小语音帧数（确认开始）
}

// DefaultConfig 默认配置
func DefaultConfig() VADConfig {
    return VADConfig{
        SampleRate:       16000,
        FrameDurationMs:  30,
        Aggressiveness:   2,
        SilenceTimeoutMs: 1000,
        PreBufferFrames:  5,
        MinSpeechFrames:  3,
    }
}

// VADState VAD 状态
type VADState int

const (
    StateIdle     VADState = iota // 空闲/静音
    StateSpeaking                 // 正在说话
)

// VADEvent VAD 事件
type VADEvent int

const (
    EventNone        VADEvent = iota // 无事件
    EventSpeechStart                 // 语音开始
    EventSpeech                      // 语音持续
    EventSpeechEnd                   // 语音结束
)

// VAD 语音活动检测器
type VAD struct {
    config VADConfig
    vad    *webrtcvad.VAD
    
    // 状态
    state         VADState
    speechFrames  int
    silenceFrames int
    
    // 预缓冲
    preBuffer [][]byte
    bufferIdx int
    
    mu sync.Mutex
}

// NewVAD 创建 VAD 实例
func NewVAD(config VADConfig) (*VAD, error) {
    vad, err := webrtcvad.New()
    if err != nil {
        return nil, err
    }
    
    if err := vad.SetMode(config.Aggressiveness); err != nil {
        return nil, err
    }
    
    return &VAD{
        config:    config,
        vad:       vad,
        state:     StateIdle,
        preBuffer: make([][]byte, config.PreBufferFrames),
    }, nil
}

// Process 处理一帧音频，返回事件和需要发送的音频
func (v *VAD) Process(frame []byte) (VADEvent, [][]byte) {
    v.mu.Lock()
    defer v.mu.Unlock()
    
    // 检测当前帧是否为语音
    isSpeech, _ := v.vad.Process(v.config.SampleRate, frame)
    
    // 更新预缓冲
    v.preBuffer[v.bufferIdx] = frame
    v.bufferIdx = (v.bufferIdx + 1) % v.config.PreBufferFrames
    
    // 计算静音超时帧数
    framesPerSecond := 1000 / v.config.FrameDurationMs
    silenceTimeoutFrames := v.config.SilenceTimeoutMs / v.config.FrameDurationMs
    
    switch v.state {
    case StateIdle:
        if isSpeech {
            v.speechFrames++
            
            // 连续语音帧达到阈值，确认说话开始
            if v.speechFrames >= v.config.MinSpeechFrames {
                v.state = StateSpeaking
                v.silenceFrames = 0
                
                // 返回预缓冲 + 当前帧
                frames := v.getPreBuffer()
                frames = append(frames, frame)
                return EventSpeechStart, frames
            }
        } else {
            v.speechFrames = 0
        }
        return EventNone, nil
        
    case StateSpeaking:
        if isSpeech {
            v.silenceFrames = 0
            return EventSpeech, [][]byte{frame}
        } else {
            v.silenceFrames++
            
            // 静音超时，确认说话结束
            if v.silenceFrames >= silenceTimeoutFrames {
                v.state = StateIdle
                v.speechFrames = 0
                v.silenceFrames = 0
                return EventSpeechEnd, nil
            }
            
            // 短暂静音，仍视为说话中
            return EventSpeech, [][]byte{frame}
        }
    }
    
    return EventNone, nil
}

// getPreBuffer 获取预缓冲的帧
func (v *VAD) getPreBuffer() [][]byte {
    frames := make([][]byte, 0, v.config.PreBufferFrames)
    
    for i := 0; i < v.config.PreBufferFrames; i++ {
        idx := (v.bufferIdx + i) % v.config.PreBufferFrames
        if v.preBuffer[idx] != nil {
            frames = append(frames, v.preBuffer[idx])
        }
    }
    
    return frames
}

// Reset 重置状态
func (v *VAD) Reset() {
    v.mu.Lock()
    defer v.mu.Unlock()
    
    v.state = StateIdle
    v.speechFrames = 0
    v.silenceFrames = 0
    v.preBuffer = make([][]byte, v.config.PreBufferFrames)
    v.bufferIdx = 0
}
```

### 使用示例

```go
func main() {
    // 创建 VAD
    config := vad.DefaultConfig()
    config.Aggressiveness = 2
    config.SilenceTimeoutMs = 1000
    
    vadDetector, err := vad.NewVAD(config)
    if err != nil {
        log.Fatal(err)
    }
    
    // 处理音频流
    for frame := range audioFrames {
        event, frames := vadDetector.Process(frame)
        
        switch event {
        case vad.EventSpeechStart:
            fmt.Println("🎤 检测到语音开始")
            // 发送 frames (包含预缓冲)
            sendToSTT(frames...)
            
        case vad.EventSpeech:
            // 发送当前帧
            sendToSTT(frames...)
            
        case vad.EventSpeechEnd:
            fmt.Println("🔇 检测到语音结束")
            // 通知 STT 语音结束
            finishSTT()
        }
    }
}
```

---

## 总结

### VAD 的核心原理

1. **人声有独特的物理特征**：谐波结构、共振峰、节奏感
2. **通过提取特征区分语音和噪声**：能量、过零率、频谱熵等
3. **使用模型进行分类**：GMM、深度学习等
4. **状态机管理语音段落**：处理开始、持续、结束

### 项目中的应用

| 组件 | 选择 | 原因 |
|------|------|------|
| VAD 算法 | WebRTC VAD | 轻量、低延时、准确率足够 |
| Aggressiveness | 2 | 平衡噪声过滤和语音保留 |
| 静音超时 | 1000ms | 适合智能家居命令场景 |
| 预缓冲 | 5帧 (150ms) | 避免首字丢失 |

---

## 参考资料

- [WebRTC VAD 源码](https://webrtc.googlesource.com/src/+/refs/heads/main/common_audio/vad/)
- [Silero VAD](https://github.com/snakers4/silero-vad)
- [语音信号处理基础](https://web.stanford.edu/~jurafsky/slp3/)
- [GMM 高斯混合模型](https://scikit-learn.org/stable/modules/mixture.html)

---

*文档版本：v1.0*
*创建日期：2026-01-08*
