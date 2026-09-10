// Package transport 定义设备通信协议层抽象。
//
// 提供 Transport 接口和 IConn 连接抽象，支持多种传输协议：
//   - WebSocket（默认，ESP32 设备接入）
//   - MQTT（可选，IoT 设备批量管理）
//
// 所有协议实现都通过 IConn 接口暴露统一的设备通信通道，
// 上层业务逻辑无需关心底层传输协议。
package transport

import (
	"context"
)

// IConn 设备连接抽象，表示一个已建立连接的设备会话。
//
// 每个 IConn 实例对应一个 ESP32 设备连接。
// 连接通过通道暴露通信数据：
//   - RecvCmd()：接收 JSON 格式的控制命令
//   - RecvAudio()：接收二进制音频数据
//   - RecvVideo()：接收二进制视频帧（camera_frame, type=0x03）
//
// 发送数据同样通过通道：
//   - SendCmd()：发送 JSON 格式的控制命令
//   - SendAudio()：发送二进制音频数据
//   - SendServo()：发送舵机控制指令
//
// v1 不下发 FaceProfile（无识别/无 PII），该方法在 v1.1+ 重启识别功能时再加回。
type IConn interface {
	// DeviceID 返回设备唯一标识（从 WebSocket URL 路径 /ws/{deviceId} 解析）。
	DeviceID() string

	// RecvCmd 返回接收 JSON 命令的通道。
	// 通道在连接关闭时关闭。
	RecvCmd() <-chan []byte

	// RecvAudio 返回接收音频数据的通道（Opus 编码）。
	// 通道在连接关闭时关闭。
	RecvAudio() <-chan []byte

	// RecvVideo 返回接收视频帧数据的通道（JPEG, type=0x03）。
	// 格式：[4B crc BE][N jpeg]，不含 envelope 头。
	// 通道在连接关闭时关闭。
	RecvVideo() <-chan []byte

	// SendCmd 发送 JSON 命令到设备。
	// 非阻塞，数据写入内部缓冲区后立即返回。
	SendCmd(data []byte) error

	// SendAudio 发送音频数据到设备（Opus 编码）。
	// 非阻塞，数据写入内部缓冲区后立即返回。
	SendAudio(data []byte) error

	// SendServo 发送舵机控制指令到设备。
	// 非阻塞，数据写入内部缓冲区后立即返回。
	SendServo(seq uint64, panUs, tiltUs, durationMs int) error

	// SendCamFps 动态调整摄像头帧率（vision-servo v2 §4.2）。
	// 设备收到后立即生效；服务端同步调整令牌桶。
	// 非阻塞。fps 范围 1-10。
	SendCamFps(fps int, reason string) error

	// SendCancel 抢占/取消指定操作（vision-servo v2 §4.2）。
	// req 为空表示取消所有 pending 操作；否则按 req 标识取消特定动作。
	// 非阻塞。
	SendCancel(req string) error

	// Close 关闭连接，释放所有资源。
	// 幂等操作，多次调用安全。
	Close() error

	// Context 返回连接上下文，连接关闭时上下文取消。
	Context() context.Context
}

// Transport 协议层抽象，负责监听设备连接。
//
// 实现类：
//   - WebSocketTransport：基于 gorilla/websocket
//   - MQTTTransport（P2 阶段）：基于 paho.mqtt.golang
type Transport interface {
	// Listen 启动监听，接收设备连接。
	// 阻塞直到 ctx 取消或发生不可恢复错误。
	Listen(ctx context.Context) error

	// OnConnect 注册连接回调。
	// 每个新建立的设备连接都会调用 handler 函数。
	// handler 在独立的 goroutine 中执行，不应阻塞。
	OnConnect(handler func(conn IConn))

	// Shutdown 优雅关闭，等待所有活跃连接处理完毕。
	// ctx 控制最大等待时间，超时后强制关闭。
	Shutdown(ctx context.Context) error
}

// ConnectHandler 新连接处理函数类型。
type ConnectHandler func(conn IConn)