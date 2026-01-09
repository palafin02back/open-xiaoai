# Open-XiaoAI HA Bridge

一个 Go 语言编写的 Open-XiaoAI Server，作为小爱音箱与 Home Assistant 之间的桥梁。

## 功能特性

- 🎤 **语音交互**：接收小爱音箱音频，通过 HA Assist Pipeline 处理
- 🏠 **智能家居控制**：利用 HA 的 Intent 匹配实现本地化控制
- 🔊 **TTS 响应**：使用 HA Piper 生成语音并回传播放
- 🔄 **多轮对话**：支持连续对话，保持上下文
- ⚡ **低延时**：流式处理，端到端延时 < 1.5s

## 快速开始

### 1. 配置

复制配置模板并修改：

```bash
cp configs/config.yaml config.yaml
```

编辑 `config.yaml`，配置 Home Assistant 地址和 Token：

```yaml
homeassistant:
  url: "http://192.168.1.100:8123"
  token: "YOUR_LONG_LIVED_ACCESS_TOKEN"
```

### 2. 运行

```bash
# 下载依赖
go mod tidy

# 运行
go run ./cmd/bridge -config config.yaml

# 或构建后运行
go build -o ha-bridge ./cmd/bridge
./ha-bridge -config config.yaml
```

### 3. 配置小爱音箱

在小爱音箱上配置 Server 地址：

```bash
echo 'ws://192.168.1.xxx:4399' > /data/open-xiaoai/server.txt
```

## 配置说明

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `server.host` | 监听地址 | `0.0.0.0` |
| `server.port` | 监听端口 | `4399` |
| `homeassistant.url` | HA 地址 | - |
| `homeassistant.token` | HA Access Token | - |
| `session.multi_turn_enabled` | 多轮对话 | `true` |
| `session.wait_speech_timeout` | 等待说话超时 | `8s` |

## Docker 部署

```bash
docker build -t ha-bridge .
docker run -d -p 4399:4399 -v ./config.yaml:/app/config.yaml ha-bridge
```

## 工作原理

```
小爱音箱 ──(WebSocket)──> HA Bridge ──(WebSocket)──> Home Assistant
   │                         │                           │
   │ 1. 唤醒事件             │                           │
   │ ─────────────────────>  │                           │
   │                         │ 2. 启动录音                │
   │ <─────────────────────  │                           │
   │ 3. 音频流               │                           │
   │ ─────────────────────>  │ 4. 运行 Pipeline          │
   │                         │ ─────────────────────>    │
   │                         │ 5. STT + Intent + TTS     │
   │                         │ <─────────────────────    │
   │ 6. TTS 音频             │                           │
   │ <─────────────────────  │                           │
```

## License

MIT
