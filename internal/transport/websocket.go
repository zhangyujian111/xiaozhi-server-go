package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ykt/xiaozhi-server-go/internal/config"
)

// WebSocketTransport 基于 gorilla/websocket 的 Transport 实现。
//
// 职责：
//   - 监听 WebSocket 连接（Gin 路由注册）
//   - 连接升级（HTTP → WebSocket）
//   - 连接生命周期管理（心跳、超时、并发限制）
//   - 将每个连接封装为 IConn 实例
//
// T9 阶段实现完整的 handler 逻辑。
// T7 阶段仅提供骨架，确保接口一致性和可编译性。
type WebSocketTransport struct {
	cfg       config.WebSocketConfig
	upgrader  websocket.Upgrader
	handler   ConnectHandler
	mu        sync.RWMutex

	// 连接管理（T9 实现）
	conns     map[string]IConn
	connCount int
}

// NewWebSocket 创建 WebSocketTransport 实例。
func NewWebSocket(cfg config.WebSocketConfig) *WebSocketTransport {
	return &WebSocketTransport{
		cfg: cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.ReadBufferSize,
			WriteBufferSize: cfg.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源（IoT 设备无浏览器同源限制）
			},
		},
		conns: make(map[string]IConn),
	}
}

// Listen 启动 WebSocket 监听。
//
// T9 实现：通过 Gin 路由注册 WebSocket 端点。
// T7 骨架：返回 nil，等待上层通过 HTTP server 注册。
func (t *WebSocketTransport) Listen(ctx context.Context) error {
	// TODO(T9): 通过 Gin 路由注册 WebSocket 端点
	//   router.GET("/ws/:deviceId", t.handleWebSocket)
	//   select { case <-ctx.Done(): return ctx.Err() }
	return nil
}

// OnConnect 注册连接回调。
func (t *WebSocketTransport) OnConnect(handler func(conn IConn)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = handler
}

// Shutdown 优雅关闭所有连接。
func (t *WebSocketTransport) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 关闭所有活跃连接
	for deviceID, conn := range t.conns {
		conn.Close()
		delete(t.conns, deviceID)
	}
	return nil
}

// handleWebSocket 处理单个 WebSocket 连接升级。
//
// T9 实现：HTTP → WebSocket 升级 + Hello 握手 + 消息循环。
func (t *WebSocketTransport) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// TODO(T9): 实现完整的 WebSocket 连接处理
	//
	// 1. 连接数检查（max_connections）
	// 2. HTTP → WebSocket 升级
	// 3. 解析 deviceId（从 URL 路径 /ws/:deviceId）
	// 4. Hello 握手（接收设备 Hello 消息，返回 session_id）
	// 5. 创建 wsConn 实例（实现 IConn 接口）
	// 6. 调用 t.handler(conn) 通知上层
	// 7. 启动消息循环 goroutine（读循环 + 写循环）
	// 8. 心跳检测（ping/pong）
}

// wsConn 实现 IConn 接口的 WebSocket 连接。
//
// T9 实现完整逻辑。
type wsConn struct {
	deviceID   string
	conn       *websocket.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	cmdCh      chan []byte
	audioCh    chan []byte
	videoCh    chan []byte
	writeCh    chan []byte
	closeOnce  sync.Once
	pingTicker *time.Ticker
}

// DeviceID 返回设备 ID。
func (c *wsConn) DeviceID() string { return c.deviceID }

// RecvCmd 返回命令接收通道。
func (c *wsConn) RecvCmd() <-chan []byte { return c.cmdCh }

// RecvAudio 返回音频接收通道。
func (c *wsConn) RecvAudio() <-chan []byte { return c.audioCh }

// RecvVideo 返回视频帧接收通道。
func (c *wsConn) RecvVideo() <-chan []byte { return c.videoCh }

// SendCmd 发送命令。
func (c *wsConn) SendCmd(data []byte) error {
	// TODO(T9): 实现非阻塞发送到 writeCh
	select {
	case c.writeCh <- data:
		return nil
	default:
		return fmt.Errorf("write channel full, dropped %d bytes", len(data))
	}
}

// SendAudio 发送音频数据。
func (c *wsConn) SendAudio(data []byte) error {
	// TODO(T9): 实现二进制帧发送
	select {
	case c.writeCh <- data:
		return nil
	default:
		return fmt.Errorf("write channel full, dropped %d bytes", len(data))
	}
}

// SendServo 发送舵机控制指令。
func (c *wsConn) SendServo(seq uint64, panUs, tiltUs, durationMs int) error {
	cmd := &ServoCommandJSON{
		Method: MethodServo,
		Params: ServoParams{
			Seq:        seq,
			PanUs:      panUs,
			TiltUs:     tiltUs,
			DurationMs: durationMs,
		},
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal servo command: %w", err)
	}
	return c.SendCmd(data)
}

// SendCamFps 发送摄像头动态调速指令（vision-servo v2 §4.2）。
func (c *wsConn) SendCamFps(fps int, reason string) error {
	cmd := &CamFpsJSON{
		Method: MethodCamFps,
		Params: CamFpsParams{
			TargetFPS: fps,
			Reason:    reason,
		},
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal cam_fps: %w", err)
	}
	return c.SendCmd(data)
}

// SendCancel 发送抢占/取消指令（vision-servo v2 §4.2）。
func (c *wsConn) SendCancel(req string) error {
	cmd := &CancelJSON{
		Method: MethodCancel,
		Params: CancelParams{
			Req:  req,
			TsMs: time.Now().UnixMilli(),
		},
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal cancel: %w", err)
	}
	return c.SendCmd(data)
}

// Close 关闭连接。
func (c *wsConn) Close() error {
	c.closeOnce.Do(func() {
		c.cancel()
		close(c.cmdCh)
		close(c.audioCh)
		close(c.videoCh)
		close(c.writeCh)
		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}
		c.conn.Close()
	})
	return nil
}

// Context 返回连接上下文。
func (c *wsConn) Context() context.Context { return c.ctx }