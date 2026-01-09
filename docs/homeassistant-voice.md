# Home Assistant 语音助手能力调研

本文档整理 Home Assistant 语音助手（Assist）的核心功能、API 接口和开发集成方式，供后续开发参考。

---

## 目录

1. [概述](#概述)
2. [核心架构](#核心架构)
3. [Assist Pipeline（语音管道）](#assist-pipeline语音管道)
4. [Speech-to-Text (STT)](#speech-to-text-stt)
5. [Text-to-Speech (TTS)](#text-to-speech-tts)
6. [Conversation API（对话处理）](#conversation-api对话处理)
7. [Wake Word（唤醒词）](#wake-word唤醒词)
8. [Wyoming 协议](#wyoming-协议)
9. [Voice Satellite（语音卫星）](#voice-satellite语音卫星)
10. [内置 Intent（意图）](#内置-intent意图)
11. [开发者 API 参考](#开发者-api-参考)
12. [集成示例](#集成示例)
13. [2024-2025 路线图](#2024-2025-路线图)

---

## 概述

Home Assistant 的语音助手功能称为 **Assist**，是一个完全本地化、隐私优先的语音控制系统。它支持：

- 🎤 **语音输入**：通过 STT 将语音转换为文本
- 🧠 **意图识别**：理解用户指令并执行智能家居控制
- 🔊 **语音输出**：通过 TTS 将响应转换为语音
- 🎯 **唤醒词**：支持自定义唤醒词触发
- 📡 **分布式部署**：支持 Voice Satellite 在多房间使用

### 主要特点

| 特性 | 说明 |
|------|------|
| **本地处理** | 所有语音处理可完全在本地完成，无需云服务 |
| **多语言支持** | 支持 50+ 种语言 |
| **可扩展** | 支持自定义 STT、TTS、Conversation Agent |
| **LLM 集成** | 可接入 OpenAI、Google 等大语言模型 |
| **开放协议** | Wyoming 协议允许第三方设备接入 |

---

## 核心架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      Assist Pipeline                             │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                                                             │ │
│  │   ┌──────────┐    ┌──────────────┐    ┌──────────────┐     │ │
│  │   │ Wake Word│ →  │Speech-to-Text│ →  │ Conversation │     │ │
│  │   │Detection │    │   (STT)      │    │   Agent      │     │ │
│  │   └──────────┘    └──────────────┘    └──────┬───────┘     │ │
│  │                                              │              │ │
│  │                                              ▼              │ │
│  │   ┌──────────┐    ┌──────────────┐    ┌──────────────┐     │ │
│  │   │  Audio   │ ←  │Text-to-Speech│ ←  │    Intent    │     │ │
│  │   │  Output  │    │   (TTS)      │    │  Execution   │     │ │
│  │   └──────────┘    └──────────────┘    └──────────────┘     │ │
│  │                                                             │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                   │
│  ┌─────────────────┐  ┌─────────────────┐  ┌────────────────┐   │
│  │   openWakeWord  │  │     Whisper     │  │     Piper      │   │
│  │  (Wake Word)    │  │  (Local STT)    │  │  (Local TTS)   │   │
│  └─────────────────┘  └─────────────────┘  └────────────────┘   │
│                                                                   │
│                    Wyoming Protocol                               │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │   Voice Satellite    Voice Satellite    Voice Satellite     │ │
│  │   (房间 1)            (房间 2)            (房间 3)           │ │
│  └─────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### 组件职责

| 组件 | 职责 | 本地方案 | 云方案 |
|------|------|----------|--------|
| **Wake Word** | 唤醒词检测 | openWakeWord, microWakeWord | - |
| **STT** | 语音转文本 | Whisper, Speech-to-Phrase | Google, Azure |
| **Conversation** | 意图识别与对话 | 内置 Intent Agent | OpenAI, Google AI |
| **TTS** | 文本转语音 | Piper | Google, Amazon Polly |

---

## Assist Pipeline（语音管道）

Assist Pipeline 是 Home Assistant 语音助手的核心集成，负责编排整个语音交互流程。

### Pipeline 配置

```yaml
# configuration.yaml
assist_pipeline:
```

### Pipeline 阶段

| 阶段 | 说明 | 事件 |
|------|------|------|
| `wake_word` | 唤醒词检测 | `wake_word-start`, `wake_word-end` |
| `stt` | 语音转文本 | `stt-start`, `stt-end` |
| `intent` | 意图识别 | `intent-start`, `intent-end` |
| `tts` | 文本转语音 | `tts-start`, `tts-end` |

### WebSocket API: 运行 Pipeline

```json
{
  "id": 1,
  "type": "assist_pipeline/run",
  "start_stage": "stt",
  "end_stage": "tts",
  "input": {
    "sample_rate": 16000
  }
}
```

**发送音频数据**：

1. 收到 `stt-start` 事件，获取 `stt_binary_handler_id`
2. 通过 WebSocket 发送二进制数据：`[handler_id_byte][audio_data]`
3. 发送结束标记：单字节 `[handler_id_byte]`

**事件响应示例**：

```json
{
  "type": "event",
  "event": {
    "type": "stt-end",
    "data": {
      "stt_output": {
        "text": "打开客厅的灯"
      }
    }
  }
}
```

---

## Speech-to-Text (STT)

### 本地 STT 方案

#### 1. Whisper Add-on

OpenAI 开源的通用语音识别模型。

```yaml
# Add-on 配置
model: base
language: zh
beam_size: 5
```

**特点**：
- 支持 100+ 语言
- 准确率高
- 资源占用较大（推荐 4GB+ 内存）

#### 2. Speech-to-Phrase

针对智能家居优化的轻量级 STT。

**特点**：
- 启动快速
- 资源占用小（适合树莓派）
- 针对家居控制短语优化

### STT API

#### REST API: `/api/stt/{provider}`

```bash
curl -X POST \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: audio/wav" \
  --data-binary @audio.wav \
  http://localhost:8123/api/stt/whisper
```

**响应**：
```json
{
  "text": "打开客厅的灯",
  "success": true
}
```

#### 自定义 STT Entity

```python
from homeassistant.components.stt import SpeechToTextEntity

class MySttEntity(SpeechToTextEntity):
    """自定义 STT 实体"""
    
    @property
    def supported_languages(self) -> list[str]:
        return ["zh-CN", "en-US"]
    
    @property
    def supported_formats(self) -> list[AudioFormats]:
        return [AudioFormats.WAV, AudioFormats.OGG]
    
    @property
    def supported_codecs(self) -> list[AudioCodecs]:
        return [AudioCodecs.PCM]
    
    @property
    def supported_bit_rates(self) -> list[AudioBitRates]:
        return [AudioBitRates.BITRATE_16]
    
    @property
    def supported_sample_rates(self) -> list[AudioSampleRates]:
        return [AudioSampleRates.SAMPLERATE_16000]
    
    @property
    def supported_channels(self) -> list[AudioChannels]:
        return [AudioChannels.CHANNEL_MONO]
    
    async def async_process_audio_stream(
        self, metadata: SpeechMetadata, stream: AsyncIterable[bytes]
    ) -> SpeechResult:
        """处理音频流并返回识别结果"""
        audio_data = b""
        async for chunk in stream:
            audio_data += chunk
        
        # 调用你的 STT 服务
        text = await my_stt_service.recognize(audio_data)
        
        return SpeechResult(
            text=text,
            result=SpeechResultState.SUCCESS
        )
```

---

## Text-to-Speech (TTS)

### 本地 TTS 方案

#### Piper

快速、高质量的本地神经网络 TTS。

```yaml
# Add-on 配置
voice: zh_CN-huayan-medium
speaker: 0
```

**支持语言**：50+ 种语言，包括多种中文声音

### TTS 服务调用

#### `tts.speak` 服务

```yaml
service: tts.speak
target:
  entity_id: tts.piper
data:
  media_player_entity_id: media_player.living_room_speaker
  message: "客厅的灯已打开"
  language: zh-CN
  options:
    voice: zh_CN-huayan-medium
```

#### REST API: 获取 TTS URL

```bash
curl -X POST \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"message": "你好", "platform": "piper", "language": "zh-CN"}' \
  http://localhost:8123/api/tts_get_url
```

**响应**：
```json
{
  "url": "/api/tts_proxy/abc123.mp3",
  "path": "/config/tts/abc123.mp3"
}
```

### 自定义 TTS Entity

```python
from homeassistant.components.tts import TextToSpeechEntity

class MyTtsEntity(TextToSpeechEntity):
    """自定义 TTS 实体"""
    
    @property
    def default_language(self) -> str:
        return "zh-CN"
    
    @property
    def supported_languages(self) -> list[str]:
        return ["zh-CN", "en-US"]
    
    @property
    def supported_options(self) -> list[str]:
        return ["voice", "speed", "pitch"]
    
    async def async_get_tts_audio(
        self, message: str, language: str, options: dict
    ) -> TtsAudioType:
        """生成 TTS 音频"""
        audio_data = await my_tts_service.synthesize(
            text=message,
            language=language,
            **options
        )
        return ("mp3", audio_data)
```

---

## Conversation API（对话处理）

Conversation API 负责理解用户意图并执行相应操作。

### REST API: `/api/conversation/process`

```bash
curl -X POST \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "text": "打开客厅的灯",
    "language": "zh-CN",
    "conversation_id": "abc123"
  }' \
  http://localhost:8123/api/conversation/process
```

**响应**：
```json
{
  "response": {
    "response_type": "action_done",
    "speech": {
      "plain": {
        "speech": "好的，已打开客厅的灯",
        "extra_data": null
      }
    },
    "card": {},
    "language": "zh-CN",
    "data": {
      "targets": [],
      "success": [
        {
          "id": "light.living_room",
          "type": "entity",
          "name": "客厅灯"
        }
      ],
      "failed": []
    }
  },
  "conversation_id": "abc123"
}
```

### WebSocket API: 对话处理

```json
{
  "id": 1,
  "type": "conversation/process",
  "text": "打开客厅的灯",
  "conversation_id": null,
  "language": "zh-CN"
}
```

### 对话代理类型

| 类型 | 说明 | 配置 |
|------|------|------|
| **内置代理** | 基于 Intent 匹配 | 默认启用 |
| **OpenAI** | GPT 模型 | 需配置 API Key |
| **Google AI** | Gemini 模型 | 需配置 API Key |
| **自定义代理** | 自行实现 | 开发集成 |

### 自定义 Conversation Entity

```python
from homeassistant.components.conversation import ConversationEntity

class MyConversationEntity(ConversationEntity):
    """自定义对话代理"""
    
    @property
    def supported_languages(self) -> list[str]:
        return ["zh-CN", "en-US"]
    
    async def async_process(
        self, user_input: ConversationInput
    ) -> ConversationResult:
        """处理用户输入"""
        # 调用你的 AI 服务
        response = await my_ai_service.chat(
            message=user_input.text,
            conversation_id=user_input.conversation_id
        )
        
        # 返回对话结果
        return ConversationResult(
            response=intent.IntentResponse(
                language=user_input.language,
                speech={"plain": {"speech": response.text}}
            ),
            conversation_id=response.conversation_id
        )
```

---

## Wake Word（唤醒词）

### openWakeWord

Home Assistant 默认的唤醒词检测系统，基于开源音频嵌入模型。

#### 内置唤醒词

- `Hey Nabu`
- `Hey Jarvis`
- `Alexa`
- `Hey Mycroft`
- `Ok Nabu`

#### 配置

```yaml
# configuration.yaml
wake_word:
```

Add-on 安装后自动被 Wyoming 集成发现。

### microWakeWord

适用于边缘设备（如 ESP32-S3）的轻量级唤醒词检测。

**特点**：
- 可在设备本地运行
- 减少网络传输
- 低延迟

### 自定义唤醒词

可以通过训练自定义唤醒词模型使用：

1. 收集唤醒词音频样本
2. 使用 openWakeWord 工具训练
3. 将模型添加到 Home Assistant

---

## Wyoming 协议

Wyoming 是 Home Assistant 设计的轻量级语音服务通信协议，用于连接外部语音服务。

### 协议特点

- **轻量级**：简单的 JSON 格式消息
- **流式传输**：支持音频流实时传输
- **服务发现**：自动发现网络中的服务

### 支持的服务类型

| 服务类型 | 说明 | 示例 |
|----------|------|------|
| `asr` | 语音转文本 | Whisper, faster-whisper |
| `tts` | 文本转语音 | Piper |
| `wake` | 唤醒词检测 | openWakeWord |
| `handle` | 意图处理 | - |

### Wyoming 消息格式

**音频开始**：
```json
{
  "type": "audio-start",
  "data": {
    "rate": 16000,
    "width": 2,
    "channels": 1
  }
}
```

**音频数据**：
```json
{
  "type": "audio-chunk",
  "data": {
    "audio": "<base64_encoded_audio>"
  }
}
```

**识别结果**：
```json
{
  "type": "transcript",
  "data": {
    "text": "打开灯"
  }
}
```

### 集成 Wyoming 服务

```yaml
# configuration.yaml
wyoming:
  - host: 192.168.1.100
    port: 10300
```

或通过 UI 添加 Wyoming 集成。

---

## Voice Satellite（语音卫星）

Voice Satellite 是分布在各个房间的语音输入/输出设备，与 Home Assistant 服务器通信。

### 支持的硬件

| 设备 | 唤醒词检测 | 特点 |
|------|------------|------|
| M5 ATOM Echo | 远程 | 低成本 |
| ESP32-S3-BOX-3 | 本地 | 内置显示屏 |
| Raspberry Pi + ReSpeaker | 远程/本地 | 高度可定制 |
| Home Assistant Voice PE | 本地 | 官方硬件 |

### Satellite 工作模式

#### 模式 1: 远程唤醒词检测

```
[Satellite] --持续音频流--> [HA Server] --唤醒词检测-->
[HA Server] --STT/TTS--> [Satellite]
```

#### 模式 2: 本地唤醒词检测

```
[Satellite] --本地唤醒词检测-->
[Satellite] --唤醒后音频--> [HA Server] --STT/TTS-->
[HA Server] --响应音频--> [Satellite]
```

### Assist Satellite 集成

```yaml
# 自动通过 ESPHome 或 Wyoming 配置
```

---

## 内置 Intent（意图）

Home Assistant 内置了丰富的意图处理器，支持常见的智能家居控制。

### 设备控制

| Intent | 功能 | 示例语句 |
|--------|------|----------|
| `HassTurnOn` | 打开设备 | "打开客厅的灯" |
| `HassTurnOff` | 关闭设备 | "关闭卧室的空调" |
| `HassGetState` | 获取状态 | "客厅的灯开着吗" |
| `HassSetPosition` | 设置位置 | "把窗帘打开到50%" |

### 灯光控制

| Intent | 功能 | 示例语句 |
|--------|------|----------|
| `HassLightSet` | 设置亮度/颜色 | "把客厅灯调到50%亮度" |

### 温控

| Intent | 功能 | 示例语句 |
|--------|------|----------|
| `HassClimateSetTemperature` | 设置温度 | "把空调调到26度" |
| `HassClimateGetTemperature` | 获取温度 | "现在室温多少度" |

### 媒体控制

| Intent | 功能 | 示例语句 |
|--------|------|----------|
| `HassMediaPause` | 暂停 | "暂停播放" |
| `HassMediaUnpause` | 继续 | "继续播放" |
| `HassMediaNext` | 下一首 | "下一首" |
| `HassMediaPrevious` | 上一首 | "上一首" |
| `HassSetVolume` | 设置音量 | "音量调到50" |
| `HassMediaSearchAndPlay` | 搜索并播放 | "播放周杰伦的歌" |

### 定时器

| Intent | 功能 | 示例语句 |
|--------|------|----------|
| `HassStartTimer` | 开始计时 | "设置10分钟的计时器" |
| `HassCancelTimer` | 取消计时 | "取消计时器" |
| `HassTimerStatus` | 查询状态 | "计时器还剩多少时间" |

### 其他

| Intent | 功能 | 示例语句 |
|--------|------|----------|
| `HassGetCurrentTime` | 获取时间 | "现在几点了" |
| `HassGetCurrentDate` | 获取日期 | "今天几号" |
| `HassGetWeather` | 获取天气 | "今天天气怎么样" |
| `HassShoppingListAddItem` | 添加购物清单 | "把牛奶加到购物清单" |
| `HassVacuumStart` | 启动扫地机器人 | "开始扫地" |
| `HassBroadcast` | 广播消息 | "广播：吃饭了" |

### 自定义 Intent

通过 `intent_script` 或 `automation` 配合 `sentence trigger` 实现：

```yaml
# configuration.yaml
intent_script:
  MakeCoffee:
    speech:
      text: "正在为您准备咖啡"
    action:
      - service: switch.turn_on
        target:
          entity_id: switch.coffee_maker
```

```yaml
# 自定义句子
# custom_sentences/zh/coffee.yaml
language: "zh"
intents:
  MakeCoffee:
    data:
      - sentences:
          - "泡杯咖啡"
          - "我要喝咖啡"
          - "做咖啡"
```

---

## 开发者 API 参考

### REST API 汇总

| 端点 | 方法 | 功能 |
|------|------|------|
| `/api/` | GET | 检查 API 状态 |
| `/api/conversation/process` | POST | 处理对话文本 |
| `/api/stt/{provider}` | POST | 语音转文本 |
| `/api/tts_get_url` | POST | 获取 TTS 音频 URL |
| `/api/services/tts/speak` | POST | 播放 TTS |

### WebSocket API 汇总

| 命令 | 功能 |
|------|------|
| `assist_pipeline/run` | 运行语音管道 |
| `assist_pipeline/pipeline/list` | 获取管道列表 |
| `conversation/process` | 处理对话 |
| `subscribe_events` | 订阅事件 |
| `call_service` | 调用服务 |

### 认证

所有 API 调用需要携带 Long-Lived Access Token：

```
Authorization: Bearer YOUR_ACCESS_TOKEN
```

获取 Token：Home Assistant UI → 个人资料 → 长期访问令牌

---

## 集成示例

### 示例 1: 发送语音指令并获取响应

```python
import aiohttp
import asyncio

async def voice_command(text: str) -> dict:
    """发送语音指令到 Home Assistant"""
    async with aiohttp.ClientSession() as session:
        async with session.post(
            "http://homeassistant.local:8123/api/conversation/process",
            headers={
                "Authorization": "Bearer YOUR_TOKEN",
                "Content-Type": "application/json"
            },
            json={
                "text": text,
                "language": "zh-CN"
            }
        ) as response:
            return await response.json()

# 使用
result = asyncio.run(voice_command("打开客厅的灯"))
print(result["response"]["speech"]["plain"]["speech"])
```

### 示例 2: 使用 WebSocket 运行完整语音管道

```python
import asyncio
import websockets
import json

async def run_voice_pipeline(audio_data: bytes):
    uri = "ws://homeassistant.local:8123/api/websocket"
    
    async with websockets.connect(uri) as ws:
        # 1. 认证
        auth_required = await ws.recv()
        await ws.send(json.dumps({
            "type": "auth",
            "access_token": "YOUR_TOKEN"
        }))
        auth_result = await ws.recv()
        
        # 2. 启动 Pipeline
        await ws.send(json.dumps({
            "id": 1,
            "type": "assist_pipeline/run",
            "start_stage": "stt",
            "end_stage": "tts",
            "input": {
                "sample_rate": 16000
            }
        }))
        
        # 3. 等待 stt-start 事件
        while True:
            msg = json.loads(await ws.recv())
            if msg.get("event", {}).get("type") == "stt-start":
                handler_id = msg["event"]["data"]["stt_binary_handler_id"]
                break
        
        # 4. 发送音频数据
        await ws.send(bytes([handler_id]) + audio_data)
        
        # 5. 发送结束标记
        await ws.send(bytes([handler_id]))
        
        # 6. 接收结果
        while True:
            msg = json.loads(await ws.recv())
            if msg.get("event", {}).get("type") == "tts-end":
                return msg["event"]["data"]
```

### 示例 3: 播放 TTS

```python
import aiohttp

async def speak(text: str, media_player: str):
    """使用 TTS 播放文字"""
    async with aiohttp.ClientSession() as session:
        await session.post(
            "http://homeassistant.local:8123/api/services/tts/speak",
            headers={
                "Authorization": "Bearer YOUR_TOKEN",
                "Content-Type": "application/json"
            },
            json={
                "entity_id": "tts.piper",
                "media_player_entity_id": media_player,
                "message": text
            }
        )
```

---

## 2024-2025 路线图

### 2024 年已实现功能

- ✅ **改进开箱即用体验**：定时器、提醒、音乐控制
- ✅ **简化配置**：更容易的初始设置
- ✅ **自定义响应**：通过自动化创建自定义语音响应
- ✅ **Voice PE 硬件**：官方语音助手硬件发布
- ✅ **LLM 回退机制**：本地优先，LLM 作为备选

### 2025 年计划功能

- 🔄 **对话式 Assist**：支持追问和多轮对话
- 🔄 **Ask Question Action**：在自动化中询问用户问题
- 🔄 **多唤醒词支持**：单设备支持两个唤醒词
- 🔄 **Collective Intelligence**：设备上下文理解
- 🔄 **智能区域响应**：同区域操作使用简短确认音

### 未来展望

- 💡 持续的 LLM 集成改进
- 💡 更多语言和声音支持
- 💡 更智能的上下文理解
- 💡 与第三方助手的互操作性

---

## 相关资源

### 官方文档

- [Home Assistant Voice](https://www.home-assistant.io/voice_control/)
- [Assist Pipeline](https://www.home-assistant.io/integrations/assist_pipeline/)
- [开发者文档 - Voice](https://developers.home-assistant.io/docs/voice/overview)
- [内置 Intent](https://developers.home-assistant.io/docs/intent_builtin)

### 开源项目

- [Wyoming Protocol](https://github.com/rhasspy/wyoming)
- [Piper TTS](https://github.com/rhasspy/piper)
- [openWakeWord](https://github.com/dscripka/openWakeWord)
- [Whisper](https://github.com/openai/whisper)

### 社区集成

- [Extended OpenAI Conversation](https://github.com/jekalmin/extended_openai_conversation)
- [Wyoming Faster Whisper](https://github.com/rhasspy/wyoming-faster-whisper)
- [Home Assistant Voice Satellite](https://esphome.io/projects/voice.html)

---

*文档更新日期：2026-01-08*
