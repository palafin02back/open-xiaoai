# CLAUDE.md

本文件为 Claude Code (claude.ai/code) 提供项目开发指南，详细说明各功能模块的实现方式和开发要点。

---

## 项目概述

**Open-XiaoAI** 是一个小米智能音箱固件改造框架，让小爱音箱能够接入自定义 AI（小智 AI、MiGPT、Gemini Live API），通过解锁音箱的"耳朵"和"嘴巴"实现无限可能。

### 核心架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Server 端（电脑/NAS）                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Xiaozhi AI  │  │   MiGPT     │  │  Gemini Live API    │  │
│  │  (Python)   │  │ (Node.js)   │  │     (Python)        │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                     │             │
│         └────────────────┼─────────────────────┘             │
│                          │                                   │
│                 ┌────────▼────────┐                          │
│                 │  Rust Bindings  │ ◄── PyO3/Neon            │
│                 │  (open_xiaoai)  │                          │
│                 └────────┬────────┘                          │
│                          │ WebSocket (端口 4399)             │
└──────────────────────────┼──────────────────────────────────┘
                           │
                    ═══════╪═══════  网络连接
                           │
┌──────────────────────────┼──────────────────────────────────┐
│                    Client 端（小爱音箱）                      │
│                 ┌────────▼────────┐                          │
│                 │   Rust Client   │                          │
│                 │  (/data/open-   │                          │
│                 │   xiaoai/client)│                          │
│                 └────────┬────────┘                          │
│         ┌────────────────┼────────────────┐                  │
│         │                │                │                  │
│  ┌──────▼──────┐  ┌──────▼──────┐  ┌──────▼──────┐          │
│  │ 音频录制/播放 │  │  状态监控    │  │  RPC 执行   │          │
│  │   (ALSA)    │  │  (事件转发)  │  │  (Shell)   │          │
│  └─────────────┘  └─────────────┘  └─────────────┘          │
└─────────────────────────────────────────────────────────────┘
```

### 技术栈

| 组件 | 语言 | 用途 |
|------|------|------|
| `packages/client-rust` | Rust | 音箱端客户端、通信协议 |
| `packages/client-patch` | Node.js | 固件提取与补丁制作 |
| `packages/flash-tool` | Shell | 刷机工具脚本 |
| `examples/xiaozhi` | Python + Rust (PyO3) | 小智 AI 服务器 |
| `examples/migpt` | TypeScript + Rust (Neon) | MiGPT 服务器 |
| `examples/gemini` | Python + Rust (PyO3) | Gemini Live API 服务器 |
| `examples/kws` | Python + Shell | 自定义唤醒词（Sherpa-ONNX） |

---

## 核心模块详解

### 1. WebSocket 通信协议

**位置**: `packages/client-rust/src/services/connect/`

#### 1.1 消息类型定义

**文件**: `data.rs`

```rust
pub enum AppMessage {
    Request(Request),   // RPC 请求（双向）
    Response(Response), // RPC 响应
    Event(Event),       // 事件通知（单向）
    Stream(Stream),     // 二进制音频流
}
```

**四种消息类型说明**：

| 类型 | 发送方向 | 用途 | 传输格式 |
|------|----------|------|----------|
| `Request` | 双向 | RPC 方法调用 | JSON Text |
| `Response` | 双向 | RPC 调用结果 | JSON Text |
| `Event` | 单向 | 状态变化通知 | JSON Text |
| `Stream` | 双向 | 音频数据传输 | Binary (JSON with bytes) |

**Request/Response 结构**：

```rust
pub struct Request {
    pub id: String,           // UUID，用于匹配响应
    pub command: String,      // 命令名称
    pub payload: Option<Value>, // JSON 参数
}

pub struct Response {
    pub id: String,           // 与请求 ID 匹配
    pub code: Option<i32>,    // 0=成功, -1=失败
    pub msg: Option<String>,  // 错误信息
    pub data: Option<Value>,  // 返回数据
}
```

**Event 结构**（单向通知）：

```rust
pub struct Event {
    pub id: String,           // UUID
    pub event: String,        // 事件类型: "kws", "instruction", "playing"
    pub data: Option<Value>,  // 事件数据
}
```

**Stream 结构**（二进制传输）：

```rust
pub struct Stream {
    pub id: String,           // UUID
    pub tag: String,          // "record"=录音, "play"=播放
    pub bytes: Vec<u8>,       // PCM 音频数据
    pub data: Option<Value>,  // 附加元数据
}
```

#### 1.2 RPC 机制

**文件**: `rpc.rs`

RPC 模块实现了双向远程过程调用：

```rust
impl RPC {
    // 注册本地命令处理器
    pub async fn add_command<F, Fut>(&self, command: &str, handler: F)
    
    // 发起远程调用（带超时）
    pub async fn call_remote(
        &self,
        command: &str,
        payload: Option<Value>,
        timeout_millis: Option<u64>,  // 默认 10 秒
    ) -> Result<Response, AppError>
    
    // 处理收到的请求
    pub async fn on_request(&self, request: Request) -> Result<Response, AppError>
    
    // 处理收到的响应
    pub async fn on_response(&self, response: Response)
}
```

**Client 端注册的命令**：

| 命令 | 功能 | 参数示例 |
|------|------|----------|
| `get_version` | 获取版本 | 无 |
| `run_shell` | 执行 Shell 脚本 | `"echo hello"` |
| `start_play` | 开始播放音频流 | `AudioConfig` |
| `stop_play` | 停止播放 | 无 |
| `start_recording` | 开始录音 | `AudioConfig` |
| `stop_recording` | 停止录音 | 无 |

#### 1.3 消息管理器

**文件**: `message.rs`

```rust
impl MessageManager {
    // 初始化 WebSocket 连接
    pub async fn init(&self, ws_stream: WsStream)
    
    // 发送事件
    pub async fn send_event(&self, event: &str, data: Option<Value>)
    
    // 发送音频流
    pub async fn send_stream(&self, tag: &str, bytes: Vec<u8>, data: Option<Value>)
    
    // 消息处理循环
    pub async fn process_messages(&self) -> Result<(), AppError>
}
```

---

### 2. 音频模块

**位置**: `packages/client-rust/src/services/audio/`

#### 2.1 音频配置

**文件**: `config.rs`

```rust
pub struct AudioConfig {
    pub pcm: String,        // ALSA 设备名，默认 "noop"
    pub channels: i32,      // 声道数，默认 1 (单声道)
    pub bits_per_sample: i32, // 位深，默认 16
    pub sample_rate: u32,   // 采样率，输入 16000Hz，输出可调
    pub period_size: u32,   // 周期大小，默认 360
    pub buffer_size: u32,   // 缓冲大小，默认 1440
}
```

**音频参数说明**：

- **输入（麦克风）**: 16kHz, 16-bit, 单声道 PCM（硬件限制）
- **输出（扬声器）**: 支持 24kHz 播放，需 Server 端处理采样率转换
- **音量问题**: 麦克风原始音量较小，需在 Server 端做音量增益

#### 2.2 录音模块

**文件**: `record.rs`

```rust
impl AudioRecorder {
    // 开始录音并回调发送数据
    pub async fn start_recording<F, Fut>(
        &self,
        on_stream: F,           // 回调函数
        config: Option<AudioConfig>,
    ) -> Result<(), AppError>
    
    // 停止录音
    pub async fn stop_recording(&self) -> Result<(), AppError>
}
```

**实现原理**：

1. 使用 `arecord` 命令行工具进行 ALSA 录音
2. 通过 `tokio::process::Command` 异步读取 stdout
3. 缓冲达到 `buffer_size` 后触发回调
4. 音频数据通过 WebSocket Stream 发送到 Server

#### 2.3 播放模块

**文件**: `play.rs`

```rust
impl AudioPlayer {
    // 初始化播放器
    pub async fn start(&self, config: Option<AudioConfig>) -> Result<(), AppError>
    
    // 播放 PCM 数据
    pub async fn play(&self, bytes: Vec<u8>) -> Result<(), AppError>
    
    // 停止播放
    pub async fn stop(&self) -> Result<(), AppError>
}
```

**实现原理**：

1. 使用 `aplay` 命令行工具进行 ALSA 播放
2. 通过 mpsc channel 异步写入 stdin
3. 支持流式播放，每次收到数据即写入

---

### 3. 状态监控模块

**位置**: `packages/client-rust/src/services/monitor/`

#### 3.1 KWS 唤醒词监控

**文件**: `kws.rs`

**监控路径**: `/tmp/open-xiaoai/kws.log`

**日志格式**：

```
1709912345678@你好小智     # 普通唤醒词
1709912345679@__STARTED__ # KWS 进程启动
```

```rust
pub enum KwsMonitorEvent {
    Started,           // KWS 进程已启动
    Keyword(String),   // 检测到唤醒词
}
```

**工作原理**：

1. 使用 `FileMonitor` 监控文件变化
2. 解析 `timestamp@keyword` 格式
3. 触发事件回调，转发到 Server

#### 3.2 语音识别结果监控

**文件**: `instruction.rs`

**监控路径**: `/tmp/mico_aivs_lab/instruction.log`

这是小爱音箱原生语音识别的日志文件，包含：

- 语音识别结果（ASR）
- TTS 播放指令
- 播放控制指令

**关键数据结构**：

```rust
pub struct RecognizeResult {
    pub confidence: f64,   // 置信度
    pub text: String,      // 识别文本
    pub is_final: bool,    // 是否最终结果
    pub is_vad_begin: bool, // 是否开始说话
}
```

**事件解析示例**（JSON 格式）：

```json
{
  "header": {
    "namespace": "SpeechRecognizer",
    "name": "RecognizeResult"
  },
  "payload": {
    "is_final": true,
    "results": [{"text": "今天天气怎么样", "confidence": 0.95}]
  }
}
```

#### 3.3 播放状态监控

**文件**: `playing.rs`

```rust
pub enum PlayingMonitorEvent {
    Playing,  // 正在播放
    Paused,   // 已暂停
    Idle,     // 空闲
}
```

**工作原理**：

通过轮询 `mphelper mute_stat` 命令获取状态：
- 返回 `1` → Playing
- 返回 `2` → Paused
- 其他 → Idle

---

### 4. Speaker 管理器

**位置**: `packages/client-rust/src/services/speaker.rs`

这是 **Server 端调用 Client 端** 的封装层，通过 RPC 执行 Shell 命令：

```rust
impl SpeakerManager {
    // 系统控制
    pub async fn get_boot() -> Result<String, AppError>     // 获取启动分区
    pub async fn set_boot(boot_part: &str)                  // 设置启动分区
    pub async fn get_device_model()                         // 获取设备型号
    pub async fn get_device_sn()                            // 获取序列号
    
    // 播放控制
    pub async fn get_play_status()                          // 获取播放状态
    pub async fn play()                                     // 播放
    pub async fn pause()                                    // 暂停
    pub async fn play_text(text: &str)                      // TTS 播放
    pub async fn play_url(url: &str)                        // 播放 URL
    
    // 麦克风控制
    pub async fn get_mic_status()                           // 获取麦克风状态
    pub async fn mic_on()                                   // 打开麦克风
    pub async fn mic_off()                                  // 关闭麦克风
    
    // 小爱控制
    pub async fn ask_xiaoai(text: &str)                     // 执行小爱指令
    pub async fn abort_xiaoai()                             // 中断小爱运行
    pub async fn wake_up(flag: bool)                        // 唤醒/取消唤醒
}
```

**Shell 命令示例**：

| 功能 | Shell 命令 |
|------|------------|
| TTS 播放 | `/usr/sbin/tts_play.sh '文字内容'` |
| URL 播放 | `ubus call mediaplayer player_play_url '{"url":"xxx","type":1}'` |
| 唤醒小爱 | `ubus call pnshelper event_notify '{"src":1,"event":0}'` |
| 执行指令 | `ubus call mibrain ai_service '{"tts":1,"nlp":1,"nlp_text":"xxx"}'` |
| 中断小爱 | `/etc/init.d/mico_aivs_lab restart` |

---

### 5. Server 端示例

#### 5.1 Xiaozhi AI Server

**位置**: `examples/xiaozhi/`

**架构**：

```
xiaozhi/
├── src/                     # Rust 绑定层 (PyO3)
│   ├── lib.rs              # Python 模块导出
│   └── server.rs           # WebSocket 服务器
├── xiaozhi/                 # Python 业务逻辑
│   ├── xiaozhi.py          # 主应用类
│   ├── xiaoai.py           # 小爱音箱交互
│   └── services/
│       ├── audio/          # 音频处理
│       │   ├── codec.py    # 编解码
│       │   ├── kws/        # 唤醒词检测
│       │   ├── vad/        # 语音活动检测
│       │   └── stream.py   # 音频流管理
│       ├── display/        # UI 显示
│       ├── protocols/      # 通信协议
│       └── speaker.py      # 音箱控制封装
└── config.py               # 配置文件
```

**核心类说明**：

**`XiaoAI` 类**（xiaoai.py）:

```python
class XiaoAI:
    mode = "xiaoai"  # 运行模式: xiaoai/xiaozhi
    speaker = SpeakerManager()
    
    @classmethod
    def on_input_data(cls, data: bytes):
        """处理小爱音箱录音数据"""
        GlobalStream.input(audio_array.tobytes())
    
    @classmethod
    def on_output_data(cls, data: bytes):
        """发送音频到小爱音箱播放"""
        await open_xiaoai_server.on_output_data(data)
    
    @classmethod
    async def on_event(cls, event: str):
        """处理小爱音箱事件"""
        # 解析事件类型
        if event_type == "instruction":
            # 处理语音识别结果
        elif event_type == "playing":
            # 更新播放状态
```

**`XiaoZhi` 类**（xiaozhi.py）:

```python
class XiaoZhi:
    """智能音箱应用程序主类"""
    
    def run(self):
        """启动应用程序"""
        self._initialize_xiaozhi()
        self._main_loop()
    
    def _on_incoming_audio(self, data):
        """接收 AI 回复音频"""
    
    def _on_incoming_json(self, json_data):
        """接收 AI 回复文本"""
    
    def start_listening(self):
        """开始监听用户语音"""
    
    def stop_listening(self):
        """停止监听"""
```

**Python-Rust 绑定**：

```rust
// src/lib.rs
#[pyfunction]
fn on_output_data(py: Python, data: Py<PyBytes>) -> PyResult<Bound<PyAny>> {
    // 发送音频流到小爱音箱
    MessageManager::instance().send_stream("play", bytes, None).await;
}

#[pyfunction]
fn run_shell(py: Python, script: String, timeout_millis: f64) {
    // 在小爱音箱上执行 Shell 命令
    RPC::instance().call_remote("run_shell", Some(json!(script)), timeout).await
}
```

#### 5.2 MiGPT Server

**位置**: `examples/migpt/`

**架构**：

```
migpt/
├── src/                     # Rust 绑定层 (Neon)
│   └── node.rs             # Node.js 模块导出
├── migpt/                   # TypeScript 业务逻辑
│   ├── xiaoai.ts           # 主引擎类
│   ├── speaker.ts          # 音箱控制封装
│   └── open-xiaoai.ts      # Rust 绑定导入
└── config.ts               # 配置文件
```

**核心类说明**：

```typescript
// xiaoai.ts
class OpenXiaoAIEngine extends MiGPTEngine {
    speaker = OpenXiaoAISpeaker;
    
    async start(config: OpenXiaoAIConfig) {
        await super.start(config);
        // 注册全局回调
        global.RUST_CALLBACKS = {
            on_event: this.onEvent,
            on_input_data: this.onRecord,
        };
        await RustServer.start();
    }
    
    onEvent = (event: string) => {
        // 处理事件：playing, instruction, kws
    };
    
    onRecord = (data: Uint8Array) => {
        // 处理录音数据
    };
}

// speaker.ts
class SpeakerManager {
    async play({ text, url, bytes, blocking }) {}
    async wakeUp(awake, { silent }) {}
    async askXiaoAI(text, { silent }) {}
    async abortXiaoAI() {}
    async runShell(script, options) {}
}
```

#### 5.3 Gemini Live API Server

**位置**: `examples/gemini/`

**架构**：

```
gemini/
├── src/                     # Rust 绑定层 (PyO3)
├── gemini/
│   ├── gemini.py           # Gemini API 客户端
│   └── xiaoai.py           # 小爱音箱交互
└── main.py
```

**Gemini 集成**：

```python
class Gemini:
    client = genai.Client(api_key=GEMINI_API_KEY)
    
    config = types.LiveConnectConfig(
        response_modalities=[types.Modality.AUDIO],
        system_instruction="你是小爱音箱...",
        speech_config=types.SpeechConfig(
            language_code="cmn-CN",
            voice_config=types.VoiceConfig(
                prebuilt_voice_config=types.PrebuiltVoiceConfig(voice_name="Leda")
            ),
        ),
    )
    
    @classmethod
    async def send_audio(cls, data: bytes):
        """发送音频给 Gemini"""
        await cls.session.send_realtime_input(
            audio=types.Blob(data=data, mime_type="audio/pcm;rate=16000")
        )
    
    @classmethod
    async def start(cls, on_audio, on_text, set_is_speaking):
        """启动 Gemini 会话"""
        async for response in session.receive():
            if response.data:
                await on_audio(response.data)
            if response.text:
                await on_text(response.text)
```

---

### 6. 自定义唤醒词 (KWS)

**位置**: `examples/kws/`

#### 6.1 工作原理

使用 [Sherpa-ONNX](https://github.com/k2-fsa/sherpa-onnx) 在设备端进行关键词检测：

1. **模型**: 约 5MB 的轻量级中文识别模型
2. **输入**: 麦克风 PCM 音频流
3. **输出**: 写入 `/tmp/open-xiaoai/kws.log`
4. **Client 监控**: `KwsMonitor` 监控日志文件变化

#### 6.2 配置文件

**唤醒词配置** (`keywords.txt`):

```
t iān m āo j īng l íng @天猫精灵
x iǎo d ù x iǎo d ù @小度小度
```

**格式说明**: `拼音序列 @显示名称`

**欢迎语配置** (`reply.txt`):

```
主人你好，请问有什么吩咐？
https://example.com/wakeup.wav
file:///usr/share/sound-vendor/AiNiRobot/wakeup_ei_01.wav
```

#### 6.3 关键脚本

- `init.sh` - 初始化 KWS 环境
- `boot.sh` - 开机自启动脚本
- `debug.sh` - 调试脚本（实时显示识别结果）
- `keywords.py` - 唤醒词拼音转换工具

---

### 7. 固件补丁工具

**位置**: `packages/client-patch/`

#### 7.1 构建流程

```
npm run build
    │
    ├── npm run ota      ─→ 下载 OTA 固件
    │                        src/ota.ts
    │
    ├── npm run extract  ─→ 解压固件文件
    │                        src/extract.py + src/extract.sh
    │
    ├── npm run patch    ─→ 应用补丁
    │                        src/patch.sh + patches/*.patch
    │
    └── npm run squashfs ─→ 重新打包
                             src/squashfs.sh
```

#### 7.2 补丁内容

| 补丁文件 | 功能 |
|----------|------|
| `01-ssh.patch` | 启用 SSH 服务 |
| `02-login.patch` | 修改 root 登录密码 |
| `03-ota.patch` | 禁用自动 OTA 更新 |
| `04-start.patch` | 添加开机启动脚本 `/data/init.sh` |

#### 7.3 环境配置

```bash
# .env 文件
MI_USER=23333333        # 小米账号
MI_PASS=xxxxxxxxx       # 小米密码
MI_DID=小爱音箱Pro       # 设备名称/DID
SSH_PASSWORD=open-xiaoai # SSH 密码
```

---

## 开发命令速查

### Rust 客户端

```bash
cd packages/client-rust

# 交叉编译（ARMv7）
cross build --release --target armv7-unknown-linux-gnueabihf

# 输出路径
./target/armv7-unknown-linux-gnueabihf/release/client

# 代码格式化
cargo fmt

# 静态检查
cargo clippy
```

### Xiaozhi AI 服务器

```bash
cd examples/xiaozhi

# 安装依赖
uv sync --locked

# 启动服务（GUI 模式）
uv run main.py

# 启动服务（CLI 模式，支持唤醒词）
CLI=true uv run main.py

# Docker 运行
docker run -it --rm -p 4399:4399 \
  -v $(pwd)/config.py:/app/config.py \
  idootop/open-xiaoai-xiaozhi:latest
```

### MiGPT 服务器

```bash
cd examples/migpt

# 启用 pnpm
corepack enable && corepack install

# 安装依赖
pnpm install

# 编译 Rust 模块
pnpm build

# 启动服务
pnpm start

# 开发模式（build + start）
pnpm dev

# Docker 运行
docker run -it --rm -p 4399:4399 \
  -v $(pwd)/config.ts:/app/config.ts \
  idootop/open-xiaoai-migpt:latest
```

### Gemini 服务器

```bash
cd examples/gemini

# 安装依赖
uv sync --locked

# 设置 API Key
export GEMINI_API_KEY=your_api_key

# 启动服务
uv run main.py
```

### 固件补丁

```bash
cd packages/client-patch

# Docker 构建（推荐）
docker run -it --rm \
  --platform linux/amd64 \
  --env-file $(pwd)/.env \
  -v $(pwd)/assets:/app/assets \
  -v $(pwd)/patches:/app/patches \
  idootop/open-xiaoai:latest

# 本地构建
npm install
npm run build
```

---

## 小爱音箱操作

### SSH 连接

```bash
# 默认密码: open-xiaoai
ssh -o HostKeyAlgorithms=+ssh-rsa root@<speaker-ip>
```

### Client 端部署

```bash
# 在音箱上创建目录
mkdir -p /data/open-xiaoai

# 设置 Server 地址
echo 'ws://192.168.31.227:4399' > /data/open-xiaoai/server.txt

# 下载并运行启动脚本
curl -sSfL https://gitee.com/idootop/artifacts/releases/download/open-xiaoai-client/init.sh | sh

# 设置开机自启动
curl -L -o /data/init.sh https://gitee.com/idootop/artifacts/releases/download/open-xiaoai-client/boot.sh
reboot
```

### 启动分区管理

```bash
# 查看当前启动分区
fw_printenv boot_part  # boot0=补丁系统, boot1=原版系统

# 切换启动分区
fw_env -s boot_part boot0  # 或 boot1
reboot
```

### 常用调试命令

```bash
# 查看进程
ps aux | grep client

# 监控唤醒词日志
tail -f /tmp/open-xiaoai/kws.log

# 监控语音识别日志
tail -f /tmp/mico_aivs_lab/instruction.log

# 播放状态
mphelper mute_stat

# 列出音频设备
arecord -l
aplay -l
```

---

## 安全警告

### ⚠️ 重要提醒

1. **硬件兼容性**: 仅支持 **LX06（小爱音箱 Pro）** 和 **OH2P（Xiaomi 智能音箱 Pro）**
2. **Shell 执行风险**: `run_shell` 命令可执行任意脚本，勿在公网部署
3. **无加密认证**: WebSocket 通信无加密，仅限可信网络使用
4. **SSH 默认密码**: 建议修改默认密码 `open-xiaoai`
5. **非官方项目**: 可能使设备保修失效

---

## 关键文件索引

### 协议与通信

| 文件 | 说明 |
|------|------|
| `packages/client-rust/src/services/connect/data.rs` | 消息类型定义 |
| `packages/client-rust/src/services/connect/rpc.rs` | RPC 机制实现 |
| `packages/client-rust/src/services/connect/message.rs` | WebSocket 消息管理 |
| `packages/client-rust/src/services/connect/handler.rs` | 消息处理器 |

### 音频处理

| 文件 | 说明 |
|------|------|
| `packages/client-rust/src/services/audio/config.rs` | 音频配置 |
| `packages/client-rust/src/services/audio/record.rs` | 录音模块 |
| `packages/client-rust/src/services/audio/play.rs` | 播放模块 |

### 状态监控

| 文件 | 说明 |
|------|------|
| `packages/client-rust/src/services/monitor/kws.rs` | 唤醒词监控 |
| `packages/client-rust/src/services/monitor/instruction.rs` | 语音识别监控 |
| `packages/client-rust/src/services/monitor/playing.rs` | 播放状态监控 |
| `packages/client-rust/src/services/monitor/file.rs` | 文件监控基类 |

### 客户端入口

| 文件 | 说明 |
|------|------|
| `packages/client-rust/src/bin/client.rs` | Client 端主程序 |
| `packages/client-rust/src/services/speaker.rs` | 音箱控制封装 |

### Server 端绑定

| 文件 | 说明 |
|------|------|
| `examples/xiaozhi/src/lib.rs` | Python 绑定 (PyO3) |
| `examples/migpt/src/node.rs` | Node.js 绑定 (Neon) |
| `examples/gemini/src/lib.rs` | Python 绑定 (PyO3) |

### 业务逻辑

| 文件 | 说明 |
|------|------|
| `examples/xiaozhi/xiaozhi/xiaozhi.py` | Xiaozhi 主应用 |
| `examples/xiaozhi/xiaozhi/xiaoai.py` | 小爱交互封装 |
| `examples/xiaozhi/xiaozhi/services/speaker.py` | 音箱控制 |
| `examples/migpt/migpt/xiaoai.ts` | MiGPT 引擎 |
| `examples/migpt/migpt/speaker.ts` | 音箱控制 |
| `examples/gemini/gemini/gemini.py` | Gemini API 客户端 |

### 固件补丁

| 文件 | 说明 |
|------|------|
| `packages/client-patch/src/ota.ts` | OTA 下载 |
| `packages/client-patch/src/patch.sh` | 补丁应用 |
| `packages/client-patch/patches/*.patch` | 补丁文件 |

### 文档

| 文件 | 说明 |
|------|------|
| `docs/flash.md` | 刷机教程 |
| `examples/kws/README.md` | KWS 使用说明 |
| `agreement.md` | 用户协议 |

---

## 扩展开发指南

### 添加新的 RPC 命令

1. **Client 端 (Rust)**:

```rust
// packages/client-rust/src/bin/client.rs
async fn my_new_command(request: Request) -> Result<Response, AppError> {
    // 实现逻辑
    Ok(Response::from_data(json!(result)))
}

// 在 init() 中注册
rpc.add_command("my_new_command", my_new_command).await;
```

2. **Server 端调用**:

```python
# Python
result = await open_xiaoai_server.run_shell("...")  # 使用现有 run_shell
# 或直接使用 RPC.call_remote("my_new_command", payload)
```

### 添加新的事件类型

1. **Client 端发送**:

```rust
MessageManager::instance()
    .send_event("my_event", Some(json!({"key": "value"})))
    .await
```

2. **Server 端处理**:

```python
# xiaozhi/xiaoai.py
async def on_event(cls, event: str):
    if event_type == "my_event":
        # 处理新事件
```

### 添加新的 Server 示例

1. 创建目录结构参考 `examples/xiaozhi`
2. 使用 PyO3 或 Neon 绑定 Rust 模块
3. 实现 `on_input_data` 和 `on_event` 回调
4. 调用 `open_xiaoai_server.start_server()` 启动

---

*本文档持续更新，欢迎贡献！*
