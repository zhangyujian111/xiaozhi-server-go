# xiaozhi-server-go 调试管理台

针对 `xiaozhi-server-go`（Go 后端，端口 8080）的轻量调试界面，覆盖：

- **系统总览**：调用 `GET /healthz`、`/readyz`、`:9090/metrics`，展示连接数
- **OTA 升级**：调 `GET /api/device/ota`、`POST /api/device/ota/activate`、`GET /firmware/:id`
- **设备 WebSocket**：连 `ws://host:8080/ws/:deviceId`，发 hello / listen start / listen stop / abort / state / mcp / iot 等 JSON-RPC；接收 TTS/STT/LLM 流；发送 Opus/PCM 二进制帧
- **记忆管理**：通过 Vite 代理转发到 ykt-aisaas 的 `GET/POST /api/v1/memories/{deviceId}/messages`
- **会话管理**：转发到 `POST/GET /api/v1/sessions/{deviceId}` 和 `DELETE /api/v1/sessions/{sessionId}`
- **人设**：转发到 `GET /api/v1/personas/{deviceId}`
- **MCP 工具**：转发到 `POST /api/v1/mcp/{deviceId}/call`
- **连接设置**：配置 aisaas 内部 token，测试两个后端连通性

## 启动

```bash
cd D:\zyj_workspace\toy\xiaozhi-server-go\web
npm install
npm run dev          # 启动到 8082
# 或 npm run dev:8080 启动到 8082（同名 alias）
```

打开 `http://localhost:8082`。

## 启动前确认

- xiaozhi-server-go 已启动在 `:8080`
- ykt-aisaas 已启动在 `:8190`（仅当要使用 Memory/Sessions/Personas/MCP 页时需要）
- 首次打开"连接设置"，填入 ykt-aisaas 的 `X-Internal-Token`（在 `ykt-aisaas/configs/*.yaml` 中查找 `internal_token`）

## 设计取舍

- 全部 aisaas 调用走浏览器 → Vite 代理 → `:8190`，**不在 xiaozhi-server-go 加代理层**——保持 Go 后端零新增
- WebSocket 走浏览器直连 `:8080/ws/:deviceId`，Vite 用 `proxy.ws` 转发
- OTA 激活测试用真实 HTTP 调用，不绕 Go 后端代理
- 8 个页面，按 reference 项目（xiaozhi-esp32-server-golang/manager/frontend）布局风格做精简版

## 不实现的内容

以下页面**未实现**——xiaozhi-server-go 后端没有对应 HTTP 路由（参见 `docs/README.md` 与 `internal/server/server.go`），需要新写后端接口：

- 设备 CRUD（list/get/bind persona）
- 用户/角色管理
- LLM/ASR/TTS/VAD/MCP/MQTT 配置 CRUD
- OTA 固件上传/列表/删除
- Prometheus dashboard 图表（仅展示原始 metrics）