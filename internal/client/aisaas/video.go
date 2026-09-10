package aisaas

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// VideoRequest 视频流分析请求
type VideoRequest struct {
	DeviceID   string `json:"deviceId"`
	Model      string `json:"model"`
	SampleRate int    `json:"sampleRate"`
	MaxFrames  int    `json:"maxFrames"`
	Prompt     string `json:"prompt"`
}

// VideoFrameResponse 视频帧分析结果
type VideoFrameResponse struct {
	FrameNumber  int      `json:"frameNumber"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	IsKeyframe  bool      `json:"isKeyframe"`
	LatencyMs   int       `json:"latencyMs"`
	Timestamp   time.Duration `json:"timestamp"`
}

// VideoStreamFrame 视频帧数据
type VideoStreamFrame struct {
	FrameNumber int    `json:"frameNumber"`
	Timestamp   int64  `json:"timestamp"` // 毫秒
	Data        string `json:"data"`      // Base64 编码的 JPEG
}

// VideoStreamClient 视频流分析客户端
type VideoStreamClient struct {
	req       VideoRequest
	respCh    chan *VideoFrameResponse
	errCh     chan error
	closeCh   chan struct{}
	closeOnce sync.Once
}

// ResponseCh 返回结果通道
func (c *VideoStreamClient) ResponseCh() <-chan *VideoFrameResponse {
	return c.respCh
}

// ErrorCh 返回错误通道
func (c *VideoStreamClient) ErrorCh() <-chan error {
	return c.errCh
}

// Close 关闭流客户端
func (c *VideoStreamClient) Close() {
	c.closeOnce.Do(func() {
		close(c.closeCh)
	})
}

// SendFrame 发送视频帧到服务器
func (c *VideoStreamClient) SendFrame(frame *VideoStreamFrame) error {
	// 此方法在此架构中未使用，帧通过 HTTP 直接发送
	// 保留接口兼容性
	return nil
}

// AnalyzeVideoStream 发起视频流分析请求（HTTP SSE）
//
// GET /api/v1/vision/video/stream
// 返回 SSE 流式响应，包含每个帧的分析结果
func (c *Client) AnalyzeVideoStream(ctx context.Context, req VideoRequest) (*VideoStreamClient, error) {
	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 1
	}
	if req.MaxFrames <= 0 {
		req.MaxFrames = 60
	}

	// 构建 SSE URL
	url := fmt.Sprintf("%s/api/v1/vision/video/stream?deviceId=%s&model=%s&sampleRate=%d&maxFrames=%d&prompt=%s",
		c.baseURL,
		req.DeviceID,
		req.Model,
		req.SampleRate,
		req.MaxFrames,
		urlEncode(req.Prompt),
	)

	// 创建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Cache-Control", "no-cache")
	c.setAuthHeader(httpReq, "bearer")

	// 发送请求
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error: status=%d, body=%s", resp.StatusCode, string(body))
	}

	// 创建流客户端
	client := &VideoStreamClient{
		req:     req,
		respCh:  make(chan *VideoFrameResponse, 100),
		errCh:   make(chan error, 1),
		closeCh: make(chan struct{}),
	}

	// 启动 SSE 读取 goroutine
	go func() {
		defer resp.Body.Close()
		defer close(client.respCh)
		defer close(client.errCh)

		reader := bufio.NewReader(resp.Body)
		for {
			select {
			case <-client.closeCh:
				return
			default:
				line, err := reader.ReadString('\n')
				if err != nil {
					if err != io.EOF {
						select {
						case client.errCh <- fmt.Errorf("read SSE failed: %w", err):
						default:
						}
					}
					return
				}

				// 解析 SSE data 行
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "data: ") {
					continue
				}

				data := strings.TrimPrefix(line, "data: ")
				if data == "" {
					continue
				}

				var frame VideoFrameResponse
				if err := json.Unmarshal([]byte(data), &frame); err != nil {
					// 可能是错误消息
					var errMsg map[string]string
					if err := json.Unmarshal([]byte(data), &errMsg); err == nil {
						if errStr, ok := errMsg["error"]; ok {
							select {
							case client.errCh <- fmt.Errorf("stream error: %s", errStr):
							default:
							}
							return
						}
					}
					continue
				}

				select {
				case client.respCh <- &frame:
				case <-client.closeCh:
					return
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return client, nil
}

// AnalyzeVideoFile 上传视频文件进行离线分析
//
// POST /api/v1/vision/video/analyze
func (c *Client) AnalyzeVideoFile(ctx context.Context, req VideoRequest, videoData []byte, filename string) (*VideoAnalysis, error) {
	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 1
	}
	if req.MaxFrames <= 0 {
		req.MaxFrames = 60
	}

	url := c.baseURL + "/api/v1/vision/video/analyze"

	// 构建 multipart 请求
	body := &bytes.Buffer{}
	writer := newMultipartWriter(body)

	// 添加表单字段
	writer.WriteField("deviceId", req.DeviceID)
	writer.WriteField("model", req.Model)
	writer.WriteField("sampleRate", fmt.Sprintf("%d", req.SampleRate))
	writer.WriteField("maxFrames", fmt.Sprintf("%d", req.MaxFrames))
	writer.WriteField("prompt", req.Prompt)
	writer.WriteField("extractMethod", "interval")

	// 添加视频文件
	writer.AddFile("file", filename, videoData)

	// 创建 HTTP 请求
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuthHeader(httpReq, "bearer")

	// 发送请求
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	var result VideoAnalysis
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response failed: %w", err)
	}

	return &result, nil
}

// VideoAnalysis 视频分析结果
type VideoAnalysis struct {
	Frames      []*VideoFrameResponse `json:"frames"`
	Summary     string                `json:"summary"`
	TotalFrames int                   `json:"totalFrames"`
	Duration    time.Duration         `json:"duration"`
	LatencyMs   int                   `json:"latencyMs"`
}

// urlEncode URL 编码
func urlEncode(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "&", "%26")
}

// multipartWriter 简单 multipart/form-data 编写器
type multipartWriter struct {
	body    *bytes.Buffer
	boundary string
}

func newMultipartWriter(body *bytes.Buffer) *multipartWriter {
	boundary := fmt.Sprintf("----AisaasFormBoundary%d", time.Now().UnixNano())
	return &multipartWriter{
		body:    body,
		boundary: boundary,
	}
}

func (w *multipartWriter) WriteField(key, value string) {
	w.body.WriteString(fmt.Sprintf("--%s\r\n", w.boundary))
	w.body.WriteString(fmt.Sprintf("Content-Disposition: form-data; name=\"%s\"\r\n\r\n", key))
	w.body.WriteString(value + "\r\n")
}

func (w *multipartWriter) AddFile(fieldName, filename string, data []byte) {
	w.body.WriteString(fmt.Sprintf("--%s\r\n", w.boundary))
	w.body.WriteString(fmt.Sprintf("Content-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\n", fieldName, filename))
	w.body.WriteString("Content-Type: video/mp4\r\n\r\n")
	w.body.Write(data)
	w.body.WriteString("\r\n")
}

func (w *multipartWriter) FormDataContentType() string {
	return fmt.Sprintf("multipart/form-data; boundary=%s", w.boundary)
}

// AnalyzeVideoFrame 分析单帧图片
//
// POST /api/v1/vision/analyze
func (c *Client) AnalyzeVideoFrame(ctx context.Context, deviceID string, frameData string, prompt string) (*VisionResponse, error) {
	return c.AnalyzeVision(ctx, VisionRequest{
		ImageURL:  frameData,
		Prompt:    prompt,
		Model:     "qwen-vl-max",
		MaxTokens: 1024,
	})
}
