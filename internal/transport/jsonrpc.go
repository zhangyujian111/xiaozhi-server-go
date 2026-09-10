// Package transport 提供 JSON-RPC 2.0 协议编解码。
//
// 实现 JSON-RPC 2.0 规范（https://www.jsonrpc.org/specification）：
//   - 请求：{"jsonrpc":"2.0","id":N,"method":"X","params":{...}}
//   - 响应：{"jsonrpc":"2.0","id":N,"result":{...}}
//   - 错误：{"jsonrpc":"2.0","id":N,"error":{"code":-32601,"message":"..."}}
//   - 通知：{"jsonrpc":"2.0","method":"X","params":{...}}（无 id 字段）
//
// 错误码遵循标准：
//   - -32700 Parse error
//   - -32600 Invalid Request
//   - -32601 Method not found
//   - -32602 Invalid params
//   - -32603 Internal error
package transport

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 标准错误码。
const (
	ErrCodeParse          = -32700 // 解析错误：JSON 格式无效
	ErrCodeInvalidRequest = -32600 // 无效请求：不是合法的 JSON-RPC 对象
	ErrCodeMethodNotFound = -32601 // 方法未找到
	ErrCodeInvalidParams  = -32602 // 无效参数
	ErrCodeInternal       = -32603 // 内部错误
	ErrCodeServerError    = -32000 // 服务端自定义错误起始码
)

// Vision-Servo v2 协议自定义错误码（与协议 spec §4.3 一致）。
//
// 这些是 JSON-RPC error.code 的字符串值（不是数字），通过 hello ACK 错误字段返回。
// 设备收到后按 spec §4.3 表格的"客户端动作"处理。
const (
	VisionErrAuthFail              = "AUTH_FAIL"               // token 错误 / 过期 / 未授权
	VisionErrPBVerUnsupported      = "PB_VER_UNSUPPORTED"      // 协议版本不支持
	VisionErrCapabilityInsufficient = "CAPABILITY_INSUFFICIENT" // 设备 capability 不满足最低要求
	VisionErrRateLimit             = "RATE_LIMIT"              // 帧率超限 / servo 限流
	VisionErrCRCFail               = "CRC_FAIL"                // 帧 CRC 校验失败
)

// JSONRPCRequest 是 JSON-RPC 2.0 请求对象。
//
// 字段规则：
//   - jsonrpc：必须为 "2.0"
//   - id：请求标识符（整数），通知时省略
//   - method：方法名（字符串）
//   - params：方法参数（JSON RawMessage，可选）
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification 返回 true 表示这是一个通知（无 id，不需要响应）。
func (r *JSONRPCRequest) IsNotification() bool {
	return r.ID == nil
}

// JSONRPCResponse 是 JSON-RPC 2.0 响应对象。
//
// 成功响应包含 result 字段，错误响应包含 error 字段。
// 两者互斥，不会同时出现。
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError 是 JSON-RPC 2.0 错误对象。
//
// 包含错误码、错误描述和可选的错误数据。
type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error 实现 error 接口。
func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// ParseRequest 从原始 JSON 字节解析 JSON-RPC 2.0 请求。
//
// 返回错误：
//   - 解析失败：JSON 格式无效
//   - 版本无效：jsonrpc 字段不是 "2.0"
//   - 方法缺失：method 为空
func ParseRequest(data []byte) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("jsonrpc: parse error: %w", err)
	}
	if req.JSONRPC != "2.0" {
		return nil, fmt.Errorf("jsonrpc: invalid version %q, expected \"2.0\"", req.JSONRPC)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("jsonrpc: method is required")
	}
	return &req, nil
}

// NewResponse 创建 JSON-RPC 2.0 成功响应。
//
// 参数：
//   - id：请求 id（通知时为 nil）
//   - result：响应结果（将被 JSON 序列化）
func NewResponse(id *int64, result interface{}) (*JSONRPCResponse, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: marshal result: %w", err)
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	}, nil
}

// NewErrorResponse 创建 JSON-RPC 2.0 错误响应。
//
// 参数：
//   - id：请求 id（通知时为 nil）
//   - code：错误码（-32700 到 -32000）
//   - message：错误描述
//   - data：可选的错误附加数据
func NewErrorResponse(id *int64, code int, message string, data interface{}) (*JSONRPCResponse, error) {
	var rawData json.RawMessage
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: marshal error data: %w", err)
		}
		rawData = raw
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    rawData,
		},
	}, nil
}

// NewNotification 创建 JSON-RPC 2.0 通知（无 id，不需要响应）。
//
// 返回序列化后的 JSON 字节，可直接通过 SendCmd 发送。
func NewNotification(method string, params interface{}) ([]byte, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: marshal notification params: %w", err)
	}
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	}
	return json.Marshal(req)
}

// Marshal 将响应序列化为 JSON 字节。
func (r *JSONRPCResponse) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

// errorMsg 用于 JSON-RPC 错误响应中的简单错误消息。
type errorMsg struct {
	Message string `json:"message"`
}