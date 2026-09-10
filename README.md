# xiaozhi-server-go

小智 AI 伴侣的 Go 语言设备会话服务。

## 项目定位

**xiaozhi-server-go** 是 xiaozhi AI 伴侣系统的设备协议层，负责：

- **设备接入**：WebSocket / MQTT 协议，接收 ESP32 等 IoT 设备连接
- **会话管理**：管理对话生命周期、配额预扣/退款
- **协议转换**：将设备语音/文本流转换为对 ykt-aisaas 的 API 调用
- **IoT 控制**：设备指令下发、OTA 升级

**不负责**（全部委托给 ykt-aisaas）：

- ❌ LLM 推理（调 `/v1/chat/completions`）
- ❌ TTS / ASR（调 `/v1/audio/speech`、`/v1/audio/transcriptions`）
- ❌ 记忆管理（调 `/api/v1/memories/*`）
- ❌ 人设管理（调 `/api/v1/personas/*`）
- ❌ MCP 工具调用（aisaas chat 内部自动循环）
- ❌ 计量计费（aisaas 内部处理）

## 项目关系

| 项目 | 关系 | 说明 |
|------|------|------|
| `ykt-aisaas` | **强依赖** | AI 能力底座，启动时鉴权 + 运行时持续调用 |
| `xiaozhi-esp32-server-java` | **行为基线** | V1 Java 实现，业务模型参考（不直接复制） |
| `xiaozhi-esp32-server-golang` | **架构参考** | Go 风格、模块组织参考（不依赖其代码） |

## 快速开始

### 环境要求

- Go 1.21+
- Redis（可选，用于限流和缓存）
- MySQL（可选，用于本地业务数据存储）
- 可访问的 ykt-aisaas 实例

### 安装与运行

```bash
# 1. 克隆项目
git clone <repo-url> xiaozhi-server-go
cd xiaozhi-server-go

# 2. 复制配置文件
cp configs/config.yaml.example configs/config.yaml

# 3. 编辑 config.yaml
# - 修改 aisaas.url 指向你的 ykt-aisaas 实例
# - 修改 aisaas.internal_token 为有效的内部令牌
# - 根据需要配置 Redis / MySQL

# 4. 下载依赖
go mod download

# 5. 运行
go run cmd/server/main.go
```

### 配置说明

配置文件位于 `configs/config.yaml`，详细字段说明见 `configs/config.yaml.example`。

核心配置项：

| 配置项 | 说明 | 必填 |
|--------|------|:----:|
| `server.port` | HTTP/WebSocket 监听端口 | 是 |
| `aisaas.url` | ykt-aisaas 服务地址 | 是 |
| `aisaas.internal_token` | 内部接口鉴权令牌 | 是 |
| `device.mac_id` | efuse MAC 地址（设备唯一标识） | 否 |
| `redis.addr` | Redis 地址（限流 + 缓存） | 否 |
| `mysql.host` | MySQL 地址（本地业务库） | 否 |

### 设备接入流程

```
ESP32 → WebSocket ws://server:8080/ws/{deviceId}
  │
  ├─ Hello 握手（设备注册 + 鉴权）
  │
  ├─ 对话循环:
  │   ├─ 设备发送: { type: "listen", state: "start" }
  │   ├─ 设备发送: 音频数据（Opus 编码，二进制帧）
  │   ├─ 服务端处理: VAD → ASR(aisaas) → LLM(aisaas) → TTS(aisaas)
  │   └─ 服务端返回: 音频数据 + 文本
  │
  └─ 断开: 设备主动关闭或服务端超时检测
```

## 目录结构

```
xiaozhi-server-go/
├── cmd/server/main.go           # 启动入口
├── internal/
│   ├── config/                  # 配置加载（viper）
│   ├── transport/               # 协议层（Transport 接口 + WebSocket）
│   ├── client/aisaas/           # aisaas 客户端 SDK（T8 实现）
│   ├── device/                  # 设备管理 + API Key 加密存储（T10 实现）
│   ├── session/                 # 会话生命周期（P2 实现）
│   ├── audio/                   # 语音编解码（P2 实现）
│   ├── llm/                     # LLM 调用封装（调 aisaas）
│   ├── tts/                     # TTS 调用封装（调 aisaas）
│   ├── asr/                     # ASR 调用封装（调 aisaas）
│   ├── mcp/                     # MCP 客户端（P2 实现）
│   ├── iot/                     # IoT 设备控制（P2 实现）
│   ├── music/                   # 音乐播放（P2 实现）
│   ├── file/                    # 文件存储（P2 实现）
│   ├── template/                # 模板加载（P2 实现）
│   ├── observability/           # 日志 / metrics / trace
│   └── server/                  # HTTP/WebSocket server
├── pkg/utils/                   # 公共工具库
├── configs/                     # 配置文件
├── docs/                        # 项目文档
└── go.mod
```

## 开发阶段

| 阶段 | 任务 | 状态 |
|------|------|:----:|
| P1-T7 | 项目骨架 + go.mod + 配置加载 | ✅ 当前 |
| P1-T8 | aisaas 客户端 SDK | 🔜 待实现 |
| P1-T9 | 协议层（Transport + WebSocket） | 🔜 待实现 |
| P1-T10 | 设备鉴权 + API Key 管理 | 🔜 待实现 |
| P1-T11 | 启动流程集成 aisaas 鉴权 | 🔜 待实现 |
| P2 | 业务模块（session/audio/llm/tts/asr/iot/music/file） | 📋 计划中 |

## License

Internal project. All rights reserved.