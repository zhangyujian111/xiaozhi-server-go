# ESP32 固件实现文档 — 视觉跟踪子系统

> **目标读者**：负责 ESP32 固件开发的工程师
> **范围**：ESP32-S3-WROOM-1-N16R8 固件（v1）—— 设备端**只**做 JPEG 采集 + 上传，**不做**任何 CV 推理；服务端（xiaozhi-server-go + aisaas）负责**人脸检测 + 5 点关键点 + 跨帧跟踪**，再下发 servo 指令
> **不**做人脸识别 / 主人识别
> **关联文档**：
> - 协议规范：`docs/protocol/vision-servo-v1.md`（v1 wire format + 5 项安全条款）
> - 协议 §0 Target Hardware：芯片选型 + 摄像头 + 舵机接口
> - 服务端参考实现：`internal/transport` + `internal/vision`（Go 端怎么消费设备上行帧）
> - 服务端 aisaas 检测实现：`ykt-aisaas/internal/tenantm/face/detector.go`（Mock / SCRFD-10GF 切换点）
> **状态**：🟡 **DRAFT** — 实现指南，实际源码另起 repo
> **最后更新**：2026-09-09

---

## 0. 范围声明

### 0.1 v1 架构归属

| 能力 | 描述 | 算法 | 设备端 / 服务端 |
|------|------|------|-----------------|
| **JPEG 采集** | OV2640 → YUV422 → 硬件 JPEG 编码 | esp_jpeg_enc | **设备端** |
| **EXIF strip** | 去除 APP1（特别是 GPS IFD）| 自写扫描 | **设备端** |
| **CRC32 + envelope** | [4B CRC BE][JPEG payload] | 硬件 CRC32 + 位序反转 | **设备端** |
| **WebSocket + TLS** | wss://<host>/ws/<device_id>?pb_ver=2 | mbedTLS + esp_websocket_client | **设备端** |
| **人脸检测** | 在 JPEG 帧中找所有人脸位置 + 置信度 | SCRFD-10GF / YuNet（ONNX）| **服务端**（aisaas）|
| **关键点提取** | 5 点 landmark（左眼/右眼/鼻/左嘴/右嘴）| SCRFD 输出 / 独立 landmark | **服务端**（aisaas）|
| **跨帧跟踪** | 同一人脸跨多帧关联（face_id 稳定）| IoU 匹配 + EMA 平滑 + 死区 + 节流 | **服务端**（xiaozhi-server-go `FaceFollower`）|
| **舵机跟随** | 把人脸中心映射到 pan/tilt 脉冲 | 几何换算（见协议 §6.1）| **设备端**（LEDC PWM）|

### 0.2 **不**包含（v1）

- ❌ **人脸识别 / 主人识别**（不做 embedding、不做 cosine matching、不存 profile 库）
- ❌ 设备端 ML 推理（YuNet 模型、esp-dl 量化等都不需要）
- ❌ profile CRUD API（RegisterProfile / ListProfiles / DeleteProfile）
- ❌ 1:N 身份匹配
- ❌ "你好，张三"等身份播报
- ❌ face_profile 下行消息（v1 协议保留该类型但服务端**不发送**）

### 0.3 后续可加（v1.1+，**架构已留口子**）

- ✅ 设备端可加 on-device fallback（用 PSRAM 余量装小模型）
- ✅ 服务端可加轻量识别（用 aisaas `face.Embedder` 接口，已有 stub）
- ✅ 协议 `face_profile` 消息类型已保留，加 10 行代码即可恢复
- ✅ 但 v1 不实现、不部署、不暴露

---

## 1. 目标硬件

| 维度 | 规格 |
|------|------|
| **芯片** | ESP32-S3-WROOM-1-N16R8（双核 Xtensa LX7 @ 240MHz）|
| Flash | 16 MB Quad SPI |
| PSRAM | 8 MB Octal SPI |
| SRAM | 512 KB |
| 摄像头 | 推荐 **OV2640**（1MP，2-lane DVP）或 **OV5640**（5MP，自动对焦）|
| 镜头焦距 | 65° hfov（与协议 §6 一致）|
| 舵机 | 2 通道 LEDC PWM（pan/tilt，500-2500μs 范围）|
| 供电 | 5V/2A（带峰值），稳压 3.3V |

### 1.1 引脚分配（推荐）

| 功能 | GPIO | 备注 |
|------|------|------|
| 摄像头 PWDN | -1 (NC) | 摄像头常开 |
| 摄像头 RESET | -1 (NC) | 软复位 |
| XCLK | GPIO15 | 摄像头主时钟源 |
| SIOD (I2C SDA) | GPIO4 | 摄像头 SCCB |
| SIOC (I2C SCL) | GPIO5 | 摄像头 SCCB |
| D7-D0 (DVP 数据) | GPIO6-13 | 8-bit 并行 |
| VSYNC | GPIO38 |  |
| HREF | GPIO47 |  |
| PCLK | GPIO48 |  |
| 舵机 Pan | GPIO16 | LEDC channel 0 |
| 舵机 Tilt | GPIO17 | LEDC channel 1 |
| 状态 LED | GPIO48 (or D4) | 启动/连接指示 |
| 用户按键（可选）| GPIO0 | BOOT 按钮，触发 flush |

> 摄像头引脚与 S3-DevKitC 默认一致，可直接接 OV2640 模组。

---

## 2. 框架选型

### 2.1 推荐：**esp-idf v5.1+**

| 框架 | 优势 | 劣势 | 推荐度 |
|------|------|------|--------|
| **esp-idf v5.1+** | 官方，组件化，可控，OTA/TLS/PSRAM 稳定 | 学习曲线陡 | ⭐⭐⭐⭐⭐ |
| esp-who (Espressif 完整例程) | 现成 OV2640 + face_detect + LCD | 包含识别（v1 用不到）；引入大块我们不需要的代码 | ⭐⭐ |
| arduino-esp32 | 简单 | 性能/内存不可控，组件生态弱 | ⭐⭐ |

**选 esp-idf v5.1+ 原因**：
- v1 设备端**不做 ML**，所以**不需要** esp-dl / TFLite / 模型量化工具链
- PSRAM 8MB 仅用于 JPEG 双缓冲 + 临时帧缓存
- 与协议规范的 5 项 Security 条款（TLS / CRC / seq / token）需要 mbedTLS + libwebsockets 直接控制

### 2.2 依赖组件（idf_component.yml）

```yaml
dependencies:
  espressif/esp32-camera:            # OV2640 / OV5640 驱动
    version: ">=2.0.0"
  espressif/esp_jpeg_enc:            # 硬件 JPEG 编码器（S3 内置）
    version: "*"
  espressif/mbedtls:                 # TLS + HMAC + CRC32（硬件加速）
    version: "*"
  espressif/esp_websocket_client:    # WS 客户端
    version: ">=1.1.0"
  espressif/esp_lcd_panel_io:        # (可选) 屏幕显示 bbox/keypoints
```

> **不用** esp-who 完整框架：它带了 face_recognition 模块（基于 face_id 库）+ esp-dl 推理，与 v1 设备端定位冲突（v1 我们只采集、不推理）。
>
> **不用** esp-dl：v1 不需要设备端 ML；后续 v1.1+ 才考虑加 on-device fallback。

---

## 3. 固件架构

### 3.1 模块划分

```
firmware/
├── main/
│   ├── main.c                  # 入口 + 任务调度
│   ├── config.h                # 编译宏（设备ID、preshared_token 等）
│   └── secrets.h               # (git ignore) 真实 token
├── components/
│   ├── camera/                 # OV2640 驱动 + JPEG 编码
│   │   ├── camera_init.c
│   │   ├── jpeg_encode.c       # 用 esp_jpeg_enc 硬件编码
│   │   └── exif_strip.c        # 去除 GPS/EXIF（协议 §5.3）
│   ├── transport/              # 网络层
│   │   ├── wifi.c              # WiFi 连接 + NTP 同步
│   │   ├── tls.c               # mbedTLS 配置（强制 wss://）
│   │   ├── websocket.c         # WS 客户端
│   │   ├── hello_handshake.c   # hello + capability + token
│   │   └── crc32.c             # CRC32-IEEE（协议 §5.2）
│   ├── servo/                  # 舵机控制
│   │   ├── ledc_pwm.c          # LEDC 配置 + 脉宽输出
│   │   ├── seq_counter.c       # 单调递增 seq（协议 §5.4）
│   │   └── face_track_ack.c    # 回执生成（设备 → 服务端）
│   └── util/
│       ├── watchdog.c          # TWDT
│       ├── ota.c               # OTA 升级
│       └── log_redact.c        # 日志脱敏（不打印 token）
├── tools/
│   ├── mock_server.py          # 本地 mock 服务端（E2E 测试用）
│   └── flash.sh                # 编译 + 烧录
└── README.md
```

> **不再需要** `vision/`（检测）、`tracking/`（跨帧跟踪）、`face_id_table.c`：这些都在服务端做。设备端**纯透传 + 舵机执行**。

### 3.2 数据流

```
[OV2640 YUV422]
  → DMA 双缓冲 (PSRAM)
    → esp_jpeg_enc (硬件)
      → exif_strip (去除 APP1/APP13/GPS)
        → CRC32-IEEE 计算（前 4B 存 CRC，后 N-4B 存 JPEG）
          → WebSocket 二进制帧
            [4B total_len BE][1B type=0x03][4B frame_crc BE][N-4B JPEG]
              → xiaozhi-server-go
                    → aisaas face.Detect (5 点 keypoints)
                      → FaceFollower (IoU + EMA + 死区 + 节流)
                        → 下行 servo 指令 → 设备

[WebSocket JSON-RPC 上行（设备 → 服务端）]
  → dispatch(method)
    ├─ hello      → 调用 hello_handshake.c
    ├─ face_track_ack → 由 face_track_ack.c 生成（200ms 内回执 servo 执行结果）
    └─ flush      → 本地立即停止当前 servo 动作

[WebSocket JSON-RPC 下行（服务端 → 设备）]
  ws.read
    ├─ method=servo       → ledc_pwm.set(pan_us, tilt_us, duration_ms) + 200ms 内回 face_track_ack
    ├─ method=cam_fps     → camera.set_fps(target)  // 调整 OV2640 帧率
    ├─ method=cancel      → servo.cancel_pending()
    └─ method=hello_ack   → 保存 session_token + 同步 negotiated 值
```

### 3.3 任务划分（FreeRTOS）

| 任务 | 优先级 | 栈 | 职责 |
|------|--------|----|------|
| `main_task` | 5 | 4KB | 系统初始化 + 启动其他任务 |
| `camera_task` | 10 | 8KB | 持续采集 + 编码 + 发送（核心）|
| `servo_task` | 6 | 4KB | 接收 servo 指令 → LEDC PWM 输出 + 回执 |
| `ws_rx_task` | 7 | 4KB | WebSocket 收包 + JSON-RPC 分发 |
| `watchdog_task` | 1 | 1KB | 每 30s 喂狗 |
| `ota_task`（按需）| 1 | 4KB | OTA 升级（idle 时）|

> PSRAM 用量预算：JPEG 缓冲 100KB × 2（双缓冲）+ 检测中间 200KB + 模型 500KB = ~800KB（PSRAM 8MB 充裕）

---

## 4. 模块实现指南

### 4.1 摄像头采集（`components/camera/`）

```c
// camera_init.c 关键点
#define CAM_PIN_XCLK    15
#define CAM_PIN_SIOD    4
#define CAM_PIN_SIOC    5
#define CAM_PIN_D7      13
// ... 其他引脚见 §1.1

void camera_init(void) {
    camera_config_t config = {
        .pin_pwdn  = -1,
        .pin_reset = -1,
        .pin_xclk  = CAM_PIN_XCLK,
        .ledc_timer = LEDC_TIMER_0,
        .ledc_channel = LEDC_CHANNEL_0,
        .pixel_format = PIXFORMAT_YUV422,    // 给硬件 JPEG 编码器用
        .frame_size = FRAMESIZE_VGA,         // 640x480
        .jpeg_quality = 10,                   // 0-63, 越小越好
        .fb_count = 2,                        // 双缓冲
        .fb_location = CAMERA_FB_IN_PSRAM,    // 关键：放 PSRAM
        .xclk_freq_hz = 20000000,
    };
    esp_err_t err = esp_camera_init(&config);
    ESP_ERROR_CHECK(err);
    
    // OV2640 特定设置：降低分辨率到 640x480（够用）
    sensor_t *s = esp_camera_sensor_get();
    s->set_framesize(s, FRAMESIZE_VGA);
    s->set_vflip(s, 0);
    s->set_hmirror(s, 0);
}
```

### 4.2 EXIF Strip（`components/camera/exif_strip.c`）

**协议 §5.3 强制要求**：JPEG 中禁止 GPS。

```c
// exif_strip.c 思路：
// 1. 解析 JPEG marker 链
// 2. 找到 APP1 (0xFFE1) 段（EXIF）和 APP13 (0xFFED) 段（IPTC）
// 3. 直接清空这些段（保留 segment header 长度不变，改写为 0x00）
// 4. 检查 APP1 段内 GPS IFD tag (0x8825)，如有则整段清除
//
// 注意：esp_jpeg_enc 输出的 JPEG 通常不含 EXIF，
// 但相机固件可能自动添加 → 必须做这一步
// 实测：用 hex 编辑器查看，确认 APP1 不存在才放心
```

**简化版（推荐）**：直接扫描所有 0xFFE1/0xFFED 段，长度改为 2（只剩 marker），后续字节填 0。
这样 JPEG 仍合法但 EXIF 被擦除。

### 4.3 CRC32（`components/transport/crc32.c`）

**协议 §5.2 强制要求**：前 4B CRC32-IEEE BE。

```c
// ESP32 硬件加速：esp_rom_crc32_le（little-endian）
// 但协议要求 big-endian 输出 → 需手动 reverse

#include "esp_rom_crc.h"

uint32_t crc32_ieee_be(const uint8_t *data, size_t len) {
    // 1. 用硬件 CRC32 计算 little-endian
    uint32_t crc = esp_rom_crc32_le(0xFFFFFFFF, data, len) ^ 0xFFFFFFFF;
    // 2. 反转位序（LE → BE）
    uint32_t be = 0;
    for (int i = 0; i < 32; i++) {
        if (crc & (1u << i)) be |= 1u << (31 - i);
    }
    return be;
}
```

### 4.4 WebSocket + TLS（`components/transport/`）

> **TLS 终止方案已确认**：Nginx 反代（详见 `docs/compliance/p0-4-tls.md` 🟢 CLOSED v1.0）
> **证书钉扎**：`<server-domain>` 公钥 SHA256 + 备份 pin（双 pin 滚动）

```c
// websocket.c 关键配置
const esp_websocket_client_config_t ws_cfg = {
    .uri = "wss://<server-domain>/ws/esp32s3-A4CF12?pb_ver=2",  // <server-domain> 由运维替换
    .cert_pem = server_cert_pem,                  // 服务器证书（PEM，仅 fallback 用）
    .cert_len = 0,                                 // 0 = 用 cert_pem
    .skip_cert_common_name_check = false,         // 强制校验 CN/SAN
    .transport = WEBSOCKET_TRANSPORT_OVER_SSL,    // 强制 wss
    .ping_interval_sec = 25,                      // 比服务端略小
    .ping_timeout_sec = 60,
    .reconnect_timeout_ms = 10000,
    .network_timeout_ms = 10000,
};

// 证书钉扎（mbedTLS 回调）
// 生成方式：python tools/extract_pin.py /etc/nginx/ssl/<server-domain>.crt
extern const char *SERVER_CERT_PIN_CURRENT;   // 当前公钥 SHA256
extern const char *SERVER_CERT_PIN_PREVIOUS; // 前一张公钥 SHA256（容忍滚动）

static esp_err_t verify_server_cert(mbedtls_x509_crt *cert, int depth, uint32_t *flags) {
    if (depth != 0) return ESP_OK;  // 只校验 leaf
    if (*flags != 0) return ESP_ERR_X509_CERT_VERIFY_FAILED;

    // 计算 leaf 公钥 SHA256，与钉扎比对
    uint8_t pin_hash[32];
    sha256(cert->subject_raw.p, cert->subject_raw.len, pin_hash);
    if (memcmp(pin_hash, CURRENT_PIN_BYTES, 32) != 0 &&
        memcmp(pin_hash, PREVIOUS_PIN_BYTES, 32) != 0) {
        return ESP_ERR_X509_CERT_VERIFY_FAILED;
    }
    return ESP_OK;
}
esp_websocket_client_handle_t client = esp_websocket_client_init(&ws_cfg);
esp_websocket_register_events(client, WEBSOCKET_EVENT_ANY, ws_event_handler, NULL);
esp_websocket_client_start(client);
```

**协议 §5.5 强制要求**：设备必须校验服务端证书，不能 `skip_cert_common_name_check = true`。
开发阶段可用 `PROTO_DEV_ALLOW_INSECURE=1` 编译宏降级（仅 dev），prod 必须 false。

### 4.5 Hello 握手（`components/transport/hello_handshake.c`）

**协议 §3.1 强制格式**：

```c
// 构造 hello JSON-RPC
cJSON *params = cJSON_CreateObject();
cJSON_AddStringToObject(params, "device_id", CONFIG_DEVICE_ID);
cJSON_AddStringToObject(params, "fw_version", "1.0.0");
cJSON_AddNumberToObject(params, "pb_ver", 2);
cJSON_AddStringToObject(params, "token", CONFIG_PRESHARED_TOKEN);  // 首次明文，wss 下安全

// capability 根级（不是 hello.params.capability）
cJSON *cap = cJSON_CreateObject();
cJSON *cam = cJSON_CreateObject();
cJSON_AddNumberToObject(cam, "max_width", 640);
cJSON_AddNumberToObject(cam, "max_height", 480);
cJSON_AddNumberToObject(cam, "max_fps", 10);
cJSON_AddNumberToObject(cam, "jpeg_quality", 70);
cJSON_AddItemToObject(cap, "camera", cam);

cJSON *servo = cJSON_CreateObject();
cJSON_AddItemToObject(servo, "channels", cJSON_CreateStringArray(
    (const char*[]){"pan", "tilt"}, 2));
cJSON_AddNumberToObject(servo, "min_pulse_us", 500);
cJSON_AddNumberToObject(servo, "max_pulse_us", 2500);
cJSON_AddNumberToObject(servo, "center_pan", 1500);
cJSON_AddNumberToObject(servo, "center_tilt", 1500);
cJSON_AddNumberToObject(servo, "range_pan_deg", 90);
cJSON_AddNumberToObject(servo, "range_tilt_deg", 60);
cJSON_AddItemToObject(cap, "servo", servo);

cJSON_AddItemToObject(params, "capability", cap);

cJSON *req = cJSON_CreateObject();
cJSON_AddStringToObject(req, "jsonrpc", "2.0");
cJSON_AddNumberToObject(req, "id", 1);
cJSON_AddStringToObject(req, "method", "hello");
cJSON_AddItemToObject(req, "params", params);

char *json_str = cJSON_PrintUnformatted(req);
esp_websocket_client_send_text(client, json_str, strlen(json_str), portMAX_DELAY);
free(json_str);
cJSON_Delete(req);

// 异步接收 hello_ack：
//   result.ok = true → 保存 result.session_token，覆盖 CONFIG_PRESHARED_TOKEN
//   result.negotiated.camera_fps / follow_gap_ms / dead_zone_px → 同步到本地
//   result.ok = false → 看 error.code：AUTH_FAIL / PB_VER_UNSUPPORTED / CAPABILITY_INSUFFICIENT
```

### 4.6 Camera Frame Envelope（`components/vision/frame_to_packet.c`）

**协议 §2.3 强制格式**：

```c
// 打包 [4B frame_crc BE][N jpeg]（不含 envelope 头，那是 websocket 包的 binary frame type）
size_t packet_build(uint8_t *out_buf, size_t out_cap,
                    const uint8_t *jpeg, size_t jpeg_len) {
    if (out_cap < jpeg_len + 4) return 0;
    
    uint32_t crc = crc32_ieee_be(jpeg, jpeg_len);
    out_buf[0] = (crc >> 24) & 0xFF;
    out_buf[1] = (crc >> 16) & 0xFF;
    out_buf[2] = (crc >>  8) & 0xFF;
    out_buf[3] = (crc      ) & 0xFF;
    memcpy(out_buf + 4, jpeg, jpeg_len);
    return jpeg_len + 4;
}

// 发送：binary frame type=0x03
esp_websocket_client_send_bin(client, packet, packet_len, portMAX_DELAY);
```

### 4.7 人脸检测（**v1 不在设备端**，仅做协议说明）

v1 范围：**设备端不进行 CV 推理**，只采集 + 上传 JPEG；检测 / 关键点 / 跟踪全部在服务端完成。

#### 4.7.1 服务端检测（aisaas）实现要点

参考 `ykt-aisaas/internal/tenantm/face/detector.go` 的 `Detector` 接口：

```go
// aisaas side
type Detector interface {
    Detect(ctx context.Context, jpegData []byte) ([]Detection, error)
    Name() string
}

// 当前 v1：MockDetector（固定坐标 + 5 点 keypoints）
// 未来生产环境：SCRFD-10GF（Apache 2.0）+ ONNX Runtime Go
//                或 YuNet（INT8 量化后 ~100KB）
```

设备端**不需要** esp-dl / TFLite / 模型量化工具链。

#### 4.7.2 v1.1+ 可选：设备端 on-device fallback

如果服务端延迟成为瓶颈（5fps × 200ms 检测 = 跟丢人脸），可在设备端装小模型做 fallback：

```c
// components/vision/yunet_detect.c（v1.1+ 才用）
#include "dl_detect_yunet.hpp"  // esp-dl 提供的 YuNet 包装

typedef struct {
    int x, y, w, h;
    float confidence;
    int landmarks[5][2];  // 5 个关键点
} face_box_t;

int detect_faces(const uint8_t *yuv422, int w, int h, face_box_t *out, int max) {
    dl::detect::YuNet *detector = new dl::detect::YuNet(
        "yunet_nano_int8.espdl",  // 量化模型 ~100KB
        0.5f, 0.3f, 0.3f
    );
    int n = detector->run(yuv422, out, max);
    delete detector;
    return n;
}
```

但 v1 **不实现**，架构已留 PSRAM 余量（8MB），v1.1+ 可平滑接入。

### 4.8 跨帧跟踪（**v1 服务端实现**，不在设备端）

v1 设备端**不做跨帧跟踪**，由 `xiaozhi-server-go/internal/vision/face_follower.go` 的 `FaceFollower` 实现：

- IoU 匹配 + EMA 平滑（α=0.3）+ 死区 8px + 节流 220ms
- 详细算法见 `face_follower.go:OnDetection`
- 设备端**不需要** face_id_table / iou_match / ema_smooth 模块

### 4.9 舵机控制（`components/servo/`）

**协议 §4.2.1 servo 格式**：

```c
// ledc_pwm.c 关键点
void servo_init(void) {
    ledc_timer_config_t ledc_timer = {
        .duty_resolution = LEDC_TIMER_14_BIT,  // 16384 levels @ 50Hz
        .freq_hz = 50,                          // 50Hz (20ms 周期)
        .speed_mode = LEDC_HIGH_SPEED_MODE,
        .timer_num = LEDC_TIMER_0,
    };
    ledc_timer_config(&ledc_timer);
    
    // Pan
    ledc_channel_config_t ch_pan = {
        .gpio_num = 16,
        .speed_mode = LEDC_HIGH_SPEED_MODE,
        .channel = LEDC_CHANNEL_0,
        .duty = 0,
    };
    ledc_channel_config(&ch_pan);
    // Tilt 同理 channel 1
}

// 脉冲转占空比
// 500μs = 0.5ms = 0.5/20 = 2.5% duty
// 2500μs = 2.5ms = 2.5/20 = 12.5% duty
// 14-bit 精度：duty = (pulse_us / 20000) * 16384
static inline uint32_t pulse_to_duty(int pulse_us) {
    return (uint32_t)((pulse_us / 20000.0f) * 16384.0f);
}

void servo_set_pan(int pulse_us) {
    if (pulse_us < 500) pulse_us = 500;
    if (pulse_us > 2500) pulse_us = 2500;
    ledc_set_duty(LEDC_HIGH_SPEED_MODE, LEDC_CHANNEL_0, pulse_to_duty(pulse_us));
    ledc_update_duty(LEDC_HIGH_SPEED_MODE, LEDC_CHANNEL_0);
}
```

**协议 §5.4 seq 单调递增**：

```c
// seq_counter.c
static uint64_t last_seq = 0;

bool seq_check_and_update(uint64_t new_seq) {
    if (new_seq <= last_seq) {
        // 重复或回放，丢弃
        return false;
    }
    last_seq = new_seq;
    return true;
}

// face_track_ack 时回 ts_ms（用 esp_timer_get_time）
uint64_t now_ms = esp_timer_get_time() / 1000;
```

### 4.10 死区 + 节流（`components/servo/dead_zone.c`）

**协议 §6**：死区 8px，节流 220ms。

```c
typedef struct {
    int last_send_ms;
} throttle_t;

bool should_send_servo(int face_cx, int face_cy, int img_w, int img_h, throttle_t *t) {
    int err_x = face_cx - img_w / 2;
    int err_y = face_cy - img_h / 2;
    const int DEAD_ZONE = 8;
    if (abs(err_x) < DEAD_ZONE && abs(err_y) < DEAD_ZONE) {
        return false;  // 死区内
    }
    int now_ms = esp_timer_get_time() / 1000;
    if (now_ms - t->last_send_ms < 220) {
        return false;  // 节流
    }
    t->last_send_ms = now_ms;
    return true;
}
```

### 4.11 JSON-RPC 下行分发（`components/transport/`）

**协议 §4.2 下行消息**：

```c
void on_ws_message(const char *data, int len) {
    cJSON *msg = cJSON_ParseWithLength(data, len);
    const cJSON *method = cJSON_GetObjectItem(msg, "method");
    if (!cJSON_IsString(method)) { cJSON_Delete(msg); return; }
    
    const cJSON *params = cJSON_GetObjectItem(msg, "params");
    
    if (strcmp(method->valuestring, "servo") == 0) {
        // { "seq":N, "pan_us":N, "tilt_us":N, "duration_ms":N }
        const cJSON *seq  = cJSON_GetObjectItem(params, "seq");
        const cJSON *pan  = cJSON_GetObjectItem(params, "pan_us");
        const cJSON *tilt = cJSON_GetObjectItem(params, "tilt_us");
        const cJSON *dur  = cJSON_GetObjectItem(params, "duration_ms");
        if (seq_check_and_update(seq->valueint)) {
            servo_set_pan(pan->valueint);
            servo_set_tilt(tilt->valueint);
            schedule_face_track_ack(seq->valueint, "ok", pan->valueint, tilt->valueint);
        }
    }
    else if (strcmp(method->valuestring, "cam_fps") == 0) {
        // { "target_fps":N, "reason":S }
        const cJSON *fps = cJSON_GetObjectItem(params, "target_fps");
        camera_set_fps(fps->valueint);  // 调整 OV2640 帧率
    }
    else if (strcmp(method->valuestring, "cancel") == 0) {
        // { "req":S, "ts_ms":N }
        const cJSON *req = cJSON_GetObjectItem(params, "req");
        if (strcmp(req->valuestring, "servo") == 0 || strlen(req->valuestring) == 0) {
            servo_emergency_stop();
        }
    }
    else if (strcmp(method->valuestring, "hello_ack") == 0) {
        // 握手响应：保存 session_token + 同步 negotiated 值
        const cJSON *result = cJSON_GetObjectItem(msg, "result");
        const cJSON *tok = cJSON_GetObjectItem(result, "session_token");
        const cJSON *neg = cJSON_GetObjectItem(result, "negotiated");
        save_session_token(tok->valuestring);
        apply_negotiated(neg);  // 同步 camera_fps / follow_gap_ms / dead_zone_px
    }
    // face_profile 在 v1 不处理（v1.1+）
    
    cJSON_Delete(msg);
}
```

### 4.12 Flush 发送（协议 §4.1 上行）

```c
// 用户长按 BOOT 键 3s → 发送 flush
void send_flush_message(const char *reason) {
    cJSON *req = cJSON_CreateObject();
    cJSON_AddStringToObject(req, "jsonrpc", "2.0");
    cJSON_AddStringToObject(req, "method", "flush");
    cJSON *params = cJSON_AddObjectToObject(req, "params");
    cJSON_AddStringToObject(params, "reason", reason);  // "user_command"
    char *json = cJSON_PrintUnformatted(req);
    esp_websocket_client_send_text(client, json, strlen(json), portMAX_DELAY);
    free(json);
    cJSON_Delete(req);
    
    // 本地立即清空所有 tracks
    clear_all_tracks();
}
```

### 4.13 日志脱敏（`components/util/log_redact.c`）

**合规要求**：preshared_token / session_token 永不入日志。

```c
// 用 ESP_LOGx 宏的 format string + 拦截器
// 简单方法：项目里禁用所有 ESP_LOGx，统一用自定义宏
#define LOGI(fmt, ...) my_log("I", __FILE__, __LINE__, fmt, ##__VA_ARGS__)

static void my_log(const char *level, const char *file, int line, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    char buf[256];
    vsnprintf(buf, sizeof(buf), fmt, ap);
    
    // 红线检查：出现 "token=" 或 "preshared" 字段 → 整行拒绝
    if (strstr(buf, "token=") || strstr(buf, "preshared=") || 
        strstr(buf, CONFIG_PRESHARED_TOKEN) ||                // 真实 token
        strstr(buf, current_session_token)) {                 // 真实 session
        printf("[REDACTED] log blocked at %s:%d\n", file, line);
        return;
    }
    printf("[%s] %s:%d %s\n", level, file, line, buf);
}
```

> **更安全的方案**：把 token 写入 `secrets.h`（git ignore），用 `__COUNTER__` 防泄漏，build 时 strip。

---

## 5. 编译与烧录

### 5.1 配置

```bash
# 克隆（仓库另起）
git clone https://github.com/yourorg/xiaozhi-esp32-vision.git
cd xiaozhi-esp32-vision

# 配置目标芯片
idf.py set-target esp32s3

# 配置 PSRAM
idf.py menuconfig
# → Serial flasher config → Flash size → 16 MB
# → Component config → ESP PSRAM → Octal PSRAM
# → Component config → Wi-Fi → Maximum TX power → 20 dBm
# → Component config → FreeRTOS → Tick rate (Hz) → 1000
```

### 5.2 烧录 preshared_token

**绝不进 git**。编译时通过 `secrets.h` 注入：

```c
// secrets.h（git ignore）
#define CONFIG_DEVICE_ID        "esp32s3-A4CF12"
#define CONFIG_PRESHARED_TOKEN  "abc123-real-token-from-fleet-mgmt"
#define CONFIG_WIFI_SSID        "your-wifi"
#define CONFIG_WIFI_PASSWORD    "your-password"
```

```bash
# 烧录
export IDF_TARGET=esp32s3
export ESPPORT=/dev/ttyUSB0   # Linux
# 或 Windows: $env:ESPPORT = "COM5"
idf.py -p $ESPPORT flash monitor
```

### 5.3 验证

```bash
# 上电后串口应看到：
# [I] main: Booting ESP32-S3 vision firmware v1.0.0
# [I] camera: OV2640 detected, VGA (640x480), 5fps
# [I] wifi: Connected to your-wifi, IP 192.168.1.42
# [I] tls: Cert verified for xiaozhi.example.com
# [I] ws: Connected to wss://xiaozhi.example.com/ws/esp32s3-A4CF12
# [I] hello: Sending hello (token=***redacted***)
# [I] hello_ack: negotiated {camera_fps: 5, follow_gap_ms: 220, dead_zone_px: 8}
# [I] detection: YuNet model loaded, 350KB
# [I] tracking: face_id table init, max=4

# 用手在镜头前移动：
# [D] detect: face[1] bbox=(200,150,80,80) conf=0.94
# [D] servo: pan=1450us tilt=1600us seq=1
# [D] detect: face[1] bbox=(210,148,80,80) conf=0.92 (matched)
# [D] servo: throttled (within 220ms)
```

---

## 6. 测试策略

### 6.1 本地 mock 服务端

不依赖真实服务端，用 Python 启一个 mock 跑 E2E：

```python
# tools/mock_server.py
import asyncio
import json
from aiohttp import web, WSMsgType

async def ws_handler(request):
    ws = web.WebSocketResponse()
    await ws.prepare(request)
    async for msg in ws:
        if msg.type == WSMsgType.TEXT:
            data = json.loads(msg.data)
            if data['method'] == 'hello':
                await ws.send_json({
                    "jsonrpc": "2.0", "id": 1,
                    "result": {
                        "ok": True,
                        "session_token": "mock-session-token",
                        "session_expires_at": "2030-01-01T00:00:00Z",
                        "negotiated": {
                            "camera_fps": 5,
                            "follow_gap_ms": 220,
                            "dead_zone_px": 8
                        }
                    }
                })
            elif data['method'] == 'face_track_ack':
                print(f"[ack] seq={data['params']['seq']} status={data['params']['status']}")
        elif msg.type == WSMsgType.BINARY:
            if len(msg.data) >= 5 and msg.data[4] == 0x03:
                jpeg_len = len(msg.data) - 5
                print(f"[frame] {jpeg_len} bytes JPEG")
                # 主动下发 servo 测试
                if jpeg_len > 1000:
                    await ws.send_json({
                        "jsonrpc": "2.0", "method": "servo",
                        "params": {"seq": 1, "pan_us": 1500, "tilt_us": 1500, "duration_ms": 220}
                    })

app = web.Application()
app.router.add_get('/ws/{device_id}', ws_handler)
web.run_app(app, port=8080)
```

### 6.2 单元测试（`components/*/test_*.c`）

- `crc32_ieee_be`：对比标准 CRC32-IEEE
- `iou_match`：空 track / 多 track / 匹配 / 错配
- `exif_strip`：含 GPS / 不含 GPS / 多 APP1
- `servo_pulse_to_duty`：边界值（500/2500/1500）

### 6.3 集成测试

| 场景 | 期望 |
|------|------|
| 启动 → 看到 hello_ack | 连接成功 |
| 移动手在镜头前 | 检测到 face，servo 不动（中心） |
| 移动到右边 | servo pan 减小（向左追） |
| 静止 1s | 不再发 servo（死区）|
| 长按 BOOT 3s | 收到 flush，track 清空 |
| WiFi 断开 | 30s 内自动重连 |
| 发送错误 CRC | 服务端丢弃，metrics 计数（验证联调）|
| 切换 2 张人脸 | face_id 切换，servo 跟最近出现的一张 |

---

## 7. 内存 & 性能预算

### 7.1 Flash 分区

```
0x000000  bootloader                (~32KB)
0x010000  partition_table           (~4KB)
0x020000  app (firmware + 资源)     (~3MB)
  - text/data/bss: ~800KB
  - JSON-RPC 解析器 + cJSON: ~80KB
  - 摄像头驱动 + JPEG 编码器: ~200KB
  - mbedTLS + WebSocket 客户端: ~400KB
0x400000  ota_0 (备升级用)          (~3MB)
0x900000  spiffs (config + log)     (~1MB)
0xA00000  nvs (preshared_token etc) (~64KB)
```

> v1 **无** model_yunet / model_landmark 分区（设备不做 ML）。
> PSRAM 8MB 主要给 JPEG 双缓冲 + YUV 缓存（~800KB），余量 ~7MB 留给 v1.1+ on-device fallback。

### 7.2 运行时内存（PSRAM）

| 用途 | 大小 | 说明 |
|------|------|------|
| JPEG 双缓冲 | 2 × 100KB = 200KB | camera fb 在 PSRAM |
| YUV422 frame 缓存 | 1 × 600KB | 摄像头到编码器的中间缓冲 |
| 预留 | ~7MB | v1.1+ on-device fallback 空间 |

> PSRAM 8MB 总用 ~800KB，余量充足。

### 7.3 性能目标

| 指标 | 目标 | 实测 |
|------|------|------|
| JPEG 编码延迟 | < 30ms（硬件编码器）| 待测 |
| 端到端（采集 → servo 指令） | < 300ms（含 200ms 服务端处理）| 待测 |
| WiFi 带宽占用 | < 500KB/s（5fps × 100KB/帧）| 取决于 JPEG 质量 |
| CPU 占用（双核） | 30-50%（无 ML）| 待测 |
| 电流（5V） | 平均 150mA，峰值 400mA（无 ML）| 待测 |

---

## 8. 与现有协议/Spec 关系

| 协议章节 | 固件模块 | 状态 |
|---------|---------|------|
| §0 Target Hardware | 引脚分配 + 摄像头 + 舵机 | 实现 |
| §2.1 URL | `transport/websocket.c` | 实现 |
| §2.2/2.3 Binary Envelope | `camera/frame_to_packet.c`（位于 transport 也可）| 实现 |
| §3.1 Hello | `transport/hello_handshake.c` | 实现 |
| §3.2 Hello ACK | `transport/on_ws_message` hello_ack 分支 | 实现 |
| §4.1.1 camera_frame | `camera/frame_to_packet.c` + 二进制发送 | 实现 |
| §4.1.3 flush | `transport/send_flush_message()` | 实现（BOOT 键触发）|
| §4.1.4 ping | WS 库内置 | 实现 |
| §4.1.5 face_track_ack | `servo/face_track_ack.c` | 实现 |
| §4.2.1 servo | `transport/on_ws_message` servo 分支 → `servo/ledc_pwm.c` | 实现 |
| §4.2.2 face_profile | **v1 不发送**（协议预留，v1.1+ 才用）| skip |
| §4.2.3 cam_fps | `transport/on_ws_message` cam_fps 分支 → `camera/set_fps()` | 实现 |
| §4.2.4 hello_ack | `transport/on_ws_message` hello_ack 分支 | 实现 |
| §4.2.5 cancel | `transport/on_ws_message` cancel 分支 → `servo/cancel_pending()` | 实现 |
| §5.1 TLS 强制 | `esp_websocket_client` + `cert_pem` + `verify_server_cert`（双 pin 钉扎）| 实现 |
| §5.2 frame_crc | `transport/crc32.c::crc32_ieee_be` | 实现 |
| §5.3 EXIF strip | `camera/exif_strip.c` | 实现 |
| §5.4 seq 单调 | `servo/seq_counter.c` | 实现 |
| §5.5 preshared + session token | `transport/hello_handshake.c` | 实现 |

**注**：协议 §6（几何参数）的几何换算、IoU 跟踪、EMA 平滑、节流等核心算法**全部在服务端**（`xiaozhi-server-go/internal/vision/face_follower.go`），不在设备端。设备端只负责按 `servo.params.{pan_us,tilt_us}` 输出 PWM 脉冲。

---

## 9. 未来扩展（v1.1+，暂不实现）

- **profile cache**：用 PSRAM 余量存最近 N 张人脸的 embedding
- **on-device embedding**：用 esp-dl 跑量化 MobileFaceNet
- **LCD 显示**：用 esp_lcd_panel 驱动小型屏幕显示 bbox
- **声学唤醒**：集成 ESP-SR 离线唤醒词
- **多设备同步**：用 esp_now 做多设备协同

---

## 10. 开放问题

| # | 问题 | 备注 |
|---|------|------|
| 1 | 摄像头选型：OV2640（1MP，~¥15）还是 OV5640（5MP AF，~¥45）？ | 5MP 多余，**推荐 OV2640** |
| 2 | 镜头焦距：65° hfov 配 3.6mm 镜头 | 可调 |
| 3 | 红外补光：是否需要夜间模式？ | 灰度图像 + IR-cut 滤光片 |
| 4 | 防水等级：室内固定 vs 便携 | 决定外壳 |
| 5 | 电源方案：USB / 18650 / 12V DC | 影响续航 |
| 6 | 出厂激活：怎么烧 token 到 NVS？| 建议用手机 app 配网（SmartConfig）|

---

**版本**：0.1 (DRAFT)
**下次评审**：实际固件启动后反馈延迟/精度数据
**配套文档**：`docs/protocol/vision-servo-v1.md`、`docs/compliance/p0-4-tls.md`（TLS 终止）、`docs/runbook/SECURITY.md`（内网 mTLS）
