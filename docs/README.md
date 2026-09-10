# xiaozhi-server-go 架构文档

> 版本：0.1.0 | 日期：2026-09-02 | 阶段：P1-T7

## 架构概览

```
┌──────────────────────────────────────────────────────────────────┐
│                        ESP32 设备                                 │
│                    (WebSocket / MQTT)                             │
└──────────────────────────┬───────────────────────────────────────┘
                           │
                           │ ws://server:8080/ws/{deviceId}
                           │ Opus 音频 + JSON 命令
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│                     xiaozhi-server-go                             │
│                                                                   │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────────┐ │
│  │ transport/  │  │   server/    │  │     observability/       │ │
│  │ (WebSocket) │◄─┤  (Gin HTTP)  │──┤  (zerolog + metrics +    │ │
│  │             │  │              │  │   OpenTelemetry)         │ │
│  └──────┬──────┘  └──────────────┘  └──────────────────────────┘ │
│         │                                                         │
│         │ IConn 接口                                              │
│         ▼                                                         │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                    device/  (设备管理)                     │    │
│  │  ┌───────────────┐  ┌──────────────────────────────────┐ │    │
│  │  │  apikey.go    │  │  AES-256-GCM 加密存储             │ │    │
│  │  │  KeyManager   │  │  HKDF(efuseMAC) → 派生密钥       │ │    │
│  │  └───────────────┘  └──────────────────────────────────┘ │    │
│  └──────────────────────────────────────────────────────────┘    │
│         │                                                         │
│         │ API Key                                                 │
│         ▼                                                         │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │              client/aisaas/  (SDK)                         │    │
│  │  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐  │    │
│  │  │ auth │ │ chat │ │ tts  │ │ asr  │ │memory│ │persona│  │    │
│  │  └──────┘ └──────┘ └──────┘ └──────┘ └──────┘ └──────┘  │    │
│  │  ┌──────┐ ┌──────┐ ┌──────┐                              │    │
│  │  │session│ │retry │ │ mcp  │                              │    │
│  │  └──────┘ └──────┘ └──────┘                              │    │
│  └──────────────────────────┬───────────────────────────────┘    │
└──────────────────────────────┼────────────────────────────────────┘
                               │
                               │ HTTP/HTTPS
                               │ Bearer <apiKey>
                               ▼
┌──────────────────────────────────────────────────────────────────┐
│                       ykt-aisaas (AI 底座)                        │
│                                                                   │
│  /v1/chat/completions    /v1/audio/speech                        │
│  /v1/audio/transcriptions  /api/v1/memories/*                    │
│  /api/v1/personas/*      /api/v1/sessions/*                      │
│  /internal/api/v1/apikeys/*                                      │
└──────────────────────────────────────────────────────────────────┘
```

## 设计原则

### 1. 薄协议层，重 AI 底座
- xiaozhi-server-go 只负责设备接入和协议转换
- 所有 AI 能力（LLM/TTS/ASR/MCP/Memory）全部委托给 ykt-aisaas
- 不实现任何 AI 模型推理逻辑

### 2. 接口抽象，协议无关
- `Transport` 接口抽象传输协议（WebSocket / MQTT）
- `IConn` 接口抽象设备连接（命令通道 + 音频通道）
- 上层业务逻辑只依赖接口，不依赖具体协议

### 3. 安全优先
- 设备 API Key 使用 AES-256-GCM 加密存储（不落盘明文）
- 密钥派生：HKDF(efuseMAC, salt="xiaozhi-key-v1", info=deviceID)
- 自动轮换：24h 定时 + 10K 请求阈值 + 401 立即轮换

### 4. 可观测性内置
- 结构化日志（zerolog → slog 适配）
- Prometheus metrics（连接数、消息数、延迟、错误率）
- OpenTelemetry 分布式追踪（aisaas 调用链追踪）

## 数据流

### 对话流程
```
1. ESP32 唤醒词检测 → WebSocket 发送 { type: "listen", state: "start" }
2. ESP32 发送音频（Opus 帧，二进制）
3. xiaozhi-server-go:
   a. VAD 处理（本地 Silero VAD）→ 检测语音起止点
   b. 调 aisaas ASR: POST /v1/audio/transcriptions
   c. 拼装 messages: [system(prompt), history, user(asr_text)]
   d. 调 aisaas LLM: POST /v1/chat/completions (stream)
   e. 流式接收 LLM token → 累积到句子边界
   f. 调 aisaas TTS: POST /v1/audio/speech
   g. WebSocket 发送音频（Opus 帧）→ ESP32 播放
4. 对话结束:
   a. 调 aisaas WriteMessage: POST /api/v1/memories/{deviceId}/messages
   b. 调 aisaas EndSession: POST /api/v1/sessions/{sessionId}/end
```

### 启动流程
```
1. 加载 config.yaml
2. 初始化日志（zerolog）
3. 验证 aisaas 连通性（Ping）
4. 设备鉴权：
   a. 读取 efuse MAC
   b. 尝试解密 configs/device.key.enc
   c. 不存在 → 调 aisaas RegisterDevice → 加密保存
   d. 验证 apiKey: GET /v1/models
5. 拉取 Persona 配置 → 缓存到内存（TTL 1h）
6. 启动后台任务（Key 轮换、Persona 缓存刷新）
7. 启动 WebSocket 监听（:8080/ws/:deviceId）
8. 等待设备连接
```

## 关键决策

| 决策 | 选项 | 结论 | 参考 |
|------|------|------|------|
| Go 模块名 | `github.com/ykt/xiaozhi-server-go` | ✅ | ADR-004 |
| 日志框架 | zerolog + slog 适配 | ✅ | 团队标准 |
| HTTP 框架 | Gin（与 ykt-aisaas 一致） | ✅ | 团队标准 |
| 配置加载 | viper（YAML + 环境变量覆盖） | ✅ | 团队标准 |
| 加密算法 | AES-256-GCM + HKDF | ✅ | Q6 决策 |
| UUID 版本 | UUID v7（时间有序） | ✅ | Q6 决策 |
| MQTT | 可选（P2 阶段启用） | ✅ | P1-DESIGN-SPEC |

## 下一步

- [ ] T8: aisaas 客户端 SDK 实现
- [ ] T9: 协议层完整实现（WebSocket handler + Hello 握手）
- [ ] T10: 设备鉴权 + API Key 管理
- [ ] T11: 启动流程集成 + 配置文件模板
- [ ] T12: 端到端测试（mock aisaas + WebSocket）
- [ ] P2: 业务模块实现（session/audio/llm/tts/asr/iot/music/file）