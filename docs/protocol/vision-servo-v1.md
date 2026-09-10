# Vision-Servo Protocol v1

> **状态**: Stable (v1.0)
> **决策归档**: ADR-048 (`agent-hub/output/software-architect-review-20260909-171323.json`)
> **范围**: ESP32 固件 ↔ xiaozhi-server-go 之间的视觉/舵机子集协议（基于 brufik_in_one_v2 v2.1 协议子集实现 + 安全加固）
> **v1 能力**: **人脸检测（detect） + 5 点关键点（keypoints） + 跨帧跟踪（track） + 舵机跟随（servo）**。**不**做人脸识别 / 主人识别。
> **不实现**（v1 子集外）: `anim[]`（表情动画）、`pb_multi`（多 persona）、`face_profile`（身份下行）— 后续 v1.1 可增量加入

## 0. 目标硬件（Target Hardware）

| 维度       | 规格                                                    |
|----------|------------------------------------------------------|
| 芯片型号    | **ESP32-S3-WROOM-1-N16R8**（双核 Xtensa LX7 @ 240MHz）|
| Flash    | 16 MB Quad SPI                                        |
| PSRAM    | 8 MB Octal SPI                                        |
| SRAM     | 512 KB                                                |
| Wi-Fi    | 802.11 b/g/n                                          |
| 摄像头接口  | 8-bit DVP（推荐 OV2640 1MP @ 640x480 或 OV5640 5MP）   |
| 舵机      | 2 通道 LEDC PWM（pan/tilt，500-2500μs）                |
| AI 加速   | LX7 向量指令（v1 未启用，预留给 v1.1 on-device fallback）|

**架构归属（v1）**：
- **服务端推理**：aisaas 负责**人脸检测 + 5 点关键点提取**，ESP32 **仅采集 JPEG + 接收 servo 指令**。
- 理由（ADR-048）：设备 OTA 不可控、模型版本管理复杂、220ms 节流对端到端延迟敏感，客户端推理难保证稳定时延。
- v1.1 可扩展：若服务端延迟成为瓶颈，可加"on-device fallback"路径（用 PSRAM 缓存最近一次检测结果做短期离线 fallback）。

**P0-2 解锁**：2026-09-09 用户确认芯片型号。

---

## 1. 设计目标

1. **复用现有单 WS 传输**：沿用 xiaozhi-server-go 的单条 WebSocket 连接承载音频 + JSON-RPC + 视频帧（brufik 风格）。
2. **服务端主导闭环**：摄像头帧 → 服务端 **detect（aisaas）→ 5 点 keypoints** → 跨帧跟踪（FaceFollower）→ 几何转舵机指令 → 下行。设备不跑 ML。
3. **不做识别**：v1 范围内**不**做 embedding / cosine matching / profile 匹配 / 主人识别。`face_profile` 消息在 v1 中**保留**但**不发送**（为 v1.1 留接口）。
4. **子集优先**：只保留核心 6 类消息（camera_frame / servo / face_track_ack / cam_fps / cancel / flush），其余后续迭代加入。
5. **安全前置**：TLS 强制 + 设备 token + session token + 帧级 CRC + 舵机序号 + JPEG 净化。

---

## 2. 传输层

### 2.1 URL

```
wss://<host>:<port>/ws/<device_id>?pb_ver=2
```

- `device_id` 路径参数（与现有 xiaozhi WS 升级路径一致）
- `pb_ver=2` 协议版本查询参数（v1 = 1，v2 = 2，未来可共存）
- **TLS 强制**（Security §5.1）：dev 环境可降级为 `ws://`，staging/prod 必须 `wss://`

### 2.2 复用单 WS 帧类型

| 类型        | 用途                          | 说明                          |
|-------------|------------------------------|------------------------------|
| TextMessage | JSON-RPC 命令/响应 + 信令    | 单 UTF-8 文本帧               |
| BinaryMessage | 音频（Opus/PCM/Silero）    | `[4B len BE][1B type][N data]`（沿用 audio.go） |
| BinaryMessage | 视频帧（JPEG）              | `[4B len BE][1B type=0x03][N jpeg]`（**type 0x03 新增**） |

**关键**：JSON 与 binary 在同一 WS 上时分时复用，**binary 首字节为 `0x00-0x02` 或 `0x03`**；JSON 首字节为 `{`。

**错位策略（R3 决议）**：
- 收到期望 binary 但首字节是 `{` → 当前序列作废，丢弃后续 N 字节
- 收到 binary 帧但 `len` 与 `readLoop` 等待的 `next_bin_len` 不符 → 关闭 WS
- 错位帧**不污染** ASR 队列和 servo 队列

### 2.3 视频帧二进制 envelope（新增 type 0x03）

```
[4B len BE] [1B type=0x03] [N bytes JPEG payload]
```

- `type=0x03` 表示 Camera JPEG
- `len` 不含 envelope 自身（与 audio.go 现有约定一致）
- JPEG payload 内部**禁止**含 EXIF GPS（Security §5.3）

---

## 3. Capability 协商（Hello 阶段）

### 3.1 设备 → 服务端（Hello 消息，根级 capability）

```json
{
  "jsonrpc": "2.0",
  "method": "hello",
  "params": {
    "device_id": "esp32s3-A4CF12",
    "fw_version": "1.0.0",
    "pb_ver": 2,
    "token": "<preshared_token>",
    "capability": {
      "camera": {
        "max_width": 640,
        "max_height": 480,
        "max_fps": 10,
        "jpeg_quality": 70
      },
      "servo": {
        "channels": ["pan", "tilt"],
        "min_pulse_us": 500,
        "max_pulse_us": 2500,
        "center_pan": 1500,
        "center_tilt": 1500,
        "range_pan_deg": 90,
        "range_tilt_deg": 60,
        "invert_pan": false,
        "invert_tilt": false
      },
      "audio": { "codec": "opus", "sample_rate": 16000 }
    }
  }
}
```

> **R3 决议：capability 字段放在根级**（不嵌套 hello），因为 methodRouter 已经处理扁平结构，嵌套增加复杂度。

### 3.2 服务端 → 设备（Hello ACK）

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ok": true,
    "session_token": "<hmac_signed_session_token>",
    "session_expires_at": "2026-09-09T18:00:00Z",
    "negotiated": {
      "camera_fps": 5,
      "follow_gap_ms": 220,
      "dead_zone_px": 8
    }
  }
}
```

- `session_token` 替换原始 `preshared_token`，后续下行消息签名用
- `negotiated` 是服务端**强约束**后的实际值，设备**必须采用**（不再用自身 capability 的同名字段）
- 失败时 `result.ok = false` + `error.code`（`AUTH_FAIL`/`PB_VER_UNSUPPORTED`/`CAPABILITY_INSUFFICIENT`）

---

## 4. 消息清单

### 4.1 上行（设备 → 服务端）

| 消息              | 载体       | 频率上限  | 说明                                    |
|------------------|-----------|----------|----------------------------------------|
| `hello`          | JSON      | 1 次/连接 | 握手（见 §3.1）                          |
| `camera_frame`   | Binary type=0x03 | 5 fps  | JPEG 帧，**含 envelope frame_crc**（§5.2） |
| `flush`          | JSON      | 1 Hz     | 强制服务端重置跟随状态                    |
| `ping`           | JSON      | 1 Hz     | 心跳（与现有 ws pings 共存）              |
| `face_track_ack` | JSON      | 异步     | 设备回执 servo 指令执行结果               |

#### `camera_frame` binary envelope（详细）

> 4B len + 1B type=0x03 + payload。**payload 内部前 4 字节为 `frame_crc: u32 BE`**（CRC32-IEEE，覆盖后续 N-4 字节），其后 N-4 字节为纯 JPEG。

```
[4B total_len BE][1B type=0x03][4B frame_crc BE][N-4 bytes JPEG]
```

- `frame_crc` 错误 → 服务端丢弃帧 + 计数 + 不下发 servo
- JPEG 内**禁止** EXIF GPS（设备侧在编码后做 exif_strip）

#### `face_track_ack` 格式

```json
{
  "jsonrpc": "2.0",
  "method": "face_track_ack",
  "params": {
    "seq": 42,
    "status": "ok" | "blocked" | "servo_error",
    "actual_pan_us": 1450,
    "actual_tilt_us": 1620,
    "ts_ms": 1725900000123
  }
}
```

### 4.2 下行（服务端 → 设备）

| 消息            | 载体  | 频率上限       | 说明                                       |
|----------------|------|--------------|-------------------------------------------|
| `servo`        | JSON | 5 Hz (220ms throttle) | 4 元组 `{xm, ym, x, y, ms}`（对齐 brufik 字段） |
| `cam_fps`      | JSON | 1 Hz         | 调速指令（动态降帧以节流）                    |
| `hello_ack`    | JSON | 1 次/连接    | 握手响应（见 §3.2）                          |
| `cancel`       | JSON | 异步         | `{type:"pb_cancel", req:"<id>"}` 抢占/取消 |
| ~~`face_profile`~~ | JSON | — | **v1 不发送**（保留消息类型定义以备 v1.1 识别接入；客户端可忽略）|

#### 4.2.1 `servo` 格式

```json
{
  "jsonrpc": "2.0",
  "method": "servo",
  "params": {
    "seq": 42,
    "pan_us": 1450,
    "tilt_us": 1620,
    "duration_ms": 220
  }
}
```

- `seq: u64` **单调递增**（设备侧用去重 + 重连幂等，Security §5.4）
- `pan_us` / `tilt_us` 500-2500 范围（依据 capability 协商）
- `duration_ms` 220（节流窗口，由服务端控制）

#### 4.2.2 ~~`face_profile` 格式~~（v1 不发送）

> v1 范围内**不**做人脸识别 / 主人识别。该消息类型在协议中**保留**以备 v1.1 增量接入；当前服务端**不发送**，客户端应忽略。

```json
{
  "jsonrpc": "2.0",
  "method": "face_profile",
  "params": {
    "profile_id": 7,
    "display_name": "张三",
    "confidence": 0.94,
    "is_known": true,
    "ts_ms": 1725900000123
  }
}
```

### 4.3 错误码

| code                       | 含义                            | 客户端动作            |
|----------------------------|--------------------------------|---------------------|
| `AUTH_FAIL`                | token 错误 / 过期 / 未授权        | 重新走 hello         |
| `PB_VER_UNSUPPORTED`       | 协议版本不支持                    | 降级或终止           |
| `CAPABILITY_INSUFFICIENT`  | 设备 capability 不满足最低要求    | 终止或上报           |
| `RATE_LIMIT`               | 帧率超限 / servo 限流            | 降帧或忽略           |
| `CRC_FAIL`                 | 帧 CRC 校验失败                  | 丢弃 + 重传         |

---

## 5. 安全条款（Security Hardening）

> **5 项强制要求**，任何 ESP32 固件 + xiaozhi-server-go 部署必须满足。

### 5.1 TLS 强制

- **prod/staging** 必须 `wss://`，HTTP 自动重定向到 HTTPS
- 设备侧拒绝 `ws://` 升级（dev 环境通过编译宏 `PROTO_DEV_ALLOW_INSECURE=1` 例外）
- **TLS 终止点**：Nginx 反代（详见 `docs/compliance/p0-4-tls.md` 🟢 CLOSED v1.0）
- 设备证书钉扎：`<server-domain>` 公钥 SHA256（**双 pin** 滚动策略）

### 5.2 frame_crc（防传输位翻转舵机误动作）

- 每个 `camera_frame` binary payload **前 4 字节**为 CRC32-IEEE BE（覆盖其后 N-4 字节）
- 设备侧编码后计算，server 解码后校验
- 校验失败 → 丢弃 + `xiaozhi_camera_frames_total{status="dropped"}` 计数
- 误动舵机物理风险必须为零（舵机在 CRC 通过前不动作）

### 5.3 JPEG 无 EXIF GPS

- 设备编码 JPEG 后**必须**调用 `exif_strip()` 去除 EXIF（特别是 GPS IFD）
- 服务端解析时再次校验，发现 EXIF GPS → 拒绝整帧
- 库依赖：设备端使用 ESP32 `esp_timer_get_time()` + `mbedtls` 的 EXIF 解析，或专用轻量库

### 5.4 servo seq 单调递增（重连幂等）

- `servo.params.seq` **必须**单调递增（u64）
- 设备侧维护 `{last_seq}`，收到 `<= last_seq` 视为重复 → 跳过
- 设备重连后 `seq` 继续递增（不归零），新 session_id 重置 ack 状态
- 防止恶意中间人重放导致舵机抽搐

### 5.5 设备 + session token 双层鉴权

- **预共享 token**（preshared token）：设备出厂时烧录，仅在 hello 消息中明文传输（**必须** wss）
- **session token**：hello 成功后服务端签发，HMAC-SHA256(secret, device_id+nonce+ts)，**所有下行 JSON-RPC 消息**的 `params.session_token` 字段携带该值（v1 含 servo / cam_fps / cancel / hello_ack）
- session 有效期默认 1 小时，到期前 5 分钟服务端通过 `hello_ack` 续期
- 设备侧 `preshared_token` 不入日志、不入遥测，仅 hello 发送

---

## 6. 几何参数（已收敛）

| 参数              | 默认  | 范围     | 位置               | 备注                                |
|------------------|------|---------|------------------|------------------------------------|
| 死区 `dead_zone_px` | 8 px | 8-20 px | 服务端 config     | 12 px 被 SRE 否决（SRE 提议 8 px）   |
| 节流 `follow_gap_ms` | 220 ms | 100-500 ms | 服务端 config | 物理惯性约束                        |
| `cam_fps`        | 5 fps | 1-10 fps | 协商/动态降帧     | 5 fps 平衡带宽 + 跟踪质量             |
| `hfov_deg`       | 65° | 30-120° | 设备 capability  | 用于像素偏移→角度转换               |

> **R3 决议：节流逻辑在服务端**（不在设备）。原因：设备侧延迟不可控（OTA/网络抖动），舵机物理惯性丢失指令比重复指令更危险。

### 6.1 几何转换公式

```
err_x_px = face_center_x - image_center_x
err_y_px = face_center_y - image_center_y

if |err_x_px| < dead_zone_px && |err_y_px| < dead_zone_px:
    no-op  // 死区内不动作

err_x_deg = (err_x_px / image_width) * hfov_deg
err_y_deg = (err_y_px / image_height) * vfov_deg

delta_pan_us = -err_x_deg * (range_pan_deg / 180) * (max_pulse_us - min_pulse_us) / 2
delta_tilt_us = err_y_deg  * (range_tilt_deg / 180) * (max_pulse_us - min_pulse_us) / 2

target_pan_us = clamp(center_pan + delta_pan_us, min_pulse_us, max_pulse_us)
target_tilt_us = clamp(center_tilt + delta_tilt_us, min_pulse_us, max_pulse_us)
```

---

## 7. 监控指标（Prometheus 命名）

```
xiaozhi_camera_frames_total{device_id, status="success|dropped|error"}
xiaozhi_camera_processing_duration_seconds{quantile="0.99"}
xiaozhi_servo_commands_total{device_id, mode, throttled}
xiaozhi_servo_throttle_drop_total{device_id}
xiaozhi_face_detect_total{device_id, status="hit|miss|error"}
xiaozhi_hello_auth_total{status="success|fail"}
```

**告警规则建议**（不在本 spec 范围）：
- `rate(xiaozhi_camera_frames_total{status="dropped"}[5m]) > 0.1` → CRC 异常告警
- `histogram_quantile(0.99, rate(xiaozhi_camera_processing_duration_seconds_bucket[5m])) > 0.5` → 处理延迟告警
- `rate(xiaozhi_face_detect_total{status="miss"}[5m]) / rate(xiaozhi_face_detect_total[5m]) > 0.8` → 检测率异常（可能镜头被遮挡或 aisaas 故障）

---

## 8. 协议版本兼容性

| pb_ver | 状态   | 说明                                |
|--------|------|------------------------------------|
| 1      | legacy | 仅 hello + listen + audio（无视觉）|
| 2      | **current** | 视觉子集（本文档）                |
| 3      | future | 增量 anim[] + pb_multi             |

服务端**同时支持** pb_ver=1 和 pb_ver=2，握手时按客户端能力选择。

---

## 9. 协议子集范围（v1 不做）

| 功能              | 状态     | 备注                          |
|------------------|--------|------------------------------|
| `anim[]` 表情动画  | v1.1 增量 | 需要设备端 LCD/OLED 配合      |
| `pb_multi` 多 persona | v1.1 增量 | 复杂度高      |
| **人脸识别 / 主人识别** | **v1 不做**（v1.1+ 增量）| 不存 profile 库、不发 face_profile 下行、不调用 aisaas `face.Identify` |
| 设备端 embedding  | 不做   | 算力不够，embedding 在 aisaas（仅 v1.1+ 启用）|
| 设备端 motion detect | 不做   | 噪声敏感，交给 aisaas    |
| 设备端人脸检测 | 不做   | v1 服务端推理；后续可做 on-device fallback |
| 68 点 landmarks | v1.1+ | v1 仅 5 点（两眼/鼻/两嘴角）|

---

## 10. 测试契约

集成测试覆盖：
1. **happy path**: hello → camera_frame × N → 验证 aisaas `face.Detect` 被调（5 点 keypoints 返回）→ 验证 FaceFollower 跟踪 → servo 下发
2. **detect miss**: 帧中无人脸 → aisaas 返回空 → FaceFollower 不下发 servo（保持原位置）
3. **auth fail**: 缺 token / 错 token / 过期 token 3 种场景
4. **CRC fail**: 发送错误 CRC 帧 → 验证丢弃 + 指标 +1
5. **throttle**: 100 Hz 帧 → 验证 servo 下发 ≤ 5 Hz
6. **dead zone**: 中心 ±4 px 帧 → 验证无 servo
7. **EXIF GPS**: 发送含 GPS EXIF 帧 → 验证拒绝
8. **servo replay**: 同一 seq 重复发送 → 设备侧验证去重
9. **flush**: 设备发 flush → FaceFollower 状态重置 → 下次帧不引用历史
10. **cam_fps**: 服务端发 cam_fps=2 → 设备帧率降为 2
11. **cancel**: 服务端发 cancel(req="servo") → 设备立即停止待执行 servo

---

**版本**: 1.0
**生效日期**: 2026-09-09
**下一次评审**: v1.1 增量时
