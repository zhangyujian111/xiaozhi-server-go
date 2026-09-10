package aisaas

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ============================================================
// LLM Chat
// ============================================================

// Chat 非流式对话。
//
// POST /v1/chat/completions
// 鉴权: Bearer API Key
//
// 请求格式对齐 OpenAI Chat Completions API，支持扩展参数 x_tools_mcp 和 x_knowledge_base_ids。
func (c *Client) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("aisaas: chat request is nil")
	}
	if req.Stream {
		return nil, fmt.Errorf("aisaas: use ChatStream for streaming requests")
	}

	var resp ChatResponse
	_, _, err := c.doRequest(ctx, "POST", "/v1/chat/completions", req, "bearer", &resp)
	if err != nil {
		return nil, fmt.Errorf("aisaas: chat: %w", err)
	}

	return &resp, nil
}

// ChatStream 流式对话。
//
// POST /v1/chat/completions (stream: true)
// 鉴权: Bearer API Key
//
// 通过 SSE（Server-Sent Events）流式返回对话块，每块通过 callback 回调。
// 遇到 [DONE] 标记时结束。
func (c *Client) ChatStream(ctx context.Context, req *ChatRequest, callback func(chunk *ChatStreamChunk) error) error {
	if req == nil {
		return fmt.Errorf("aisaas: chat request is nil")
	}
	if callback == nil {
		return fmt.Errorf("aisaas: chat stream callback is nil")
	}

	// 确保 stream 为 true
	streamReq := *req
	streamReq.Stream = true

	extraHeaders := map[string]string{
		"Accept": "text/event-stream",
	}

	resp, err := c.doRequestRaw(ctx, "POST", "/v1/chat/completions", &streamReq, "bearer", extraHeaders)
	if err != nil {
		return fmt.Errorf("aisaas: chat stream: %w", err)
	}
	defer resp.Body.Close()

	// 检查是否是 SSE 响应
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		// 非流式响应（可能是错误），读取 body
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aisaas: unexpected content type %s: %s", contentType, string(body))
	}

	// 逐行读取 SSE 流
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("aisaas: read stream: %w", err)
		}

		line = strings.TrimSpace(line)

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// 处理 data: 行
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// [DONE] 标记
			if data == "[DONE]" {
				break
			}

			var chunk ChatStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				c.logger.WarnContext(ctx, "aisaas: parse stream chunk failed",
					"error", err,
					"data", data,
				)
				continue
			}

			if err := callback(&chunk); err != nil {
				return fmt.Errorf("aisaas: stream callback error: %w", err)
			}
		}
	}

	return nil
}
