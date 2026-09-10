package aisaas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// VisionRequest 视觉理解请求
type VisionRequest struct {
	ImageURL  string `json:"imageUrl"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model"`
	MaxTokens int    `json:"maxTokens"`
}

// VisionResponse 视觉理解响应
type VisionResponse struct {
	Description string           `json:"description"`
	Tags        []string         `json:"tags"`
	Objects     []DetectedObject `json:"objects"`
	Text        string           `json:"text"`
	Confidence  float64          `json:"confidence"`
	LatencyMs   int              `json:"latencyMs"`
}

// DetectedObject 检测到的物体
type DetectedObject struct {
	Label       string  `json:"label"`
	Confidence  float64 `json:"confidence"`
	BoundingBox [4]int  `json:"boundingBox"`
}

// VisionCompareRequest 多图对比请求
type VisionCompareRequest struct {
	ImageURLs []string `json:"imageUrls"`
	Prompt    string   `json:"prompt"`
	Model     string   `json:"model"`
}

// VisionCompareResponse 多图对比响应
type VisionCompareResponse struct {
	Analysis    string   `json:"analysis"`
	Similarity  float64  `json:"similarity"`
	Differences []string `json:"differences"`
	LatencyMs   int      `json:"latencyMs"`
}

// AnalyzeVision 单图理解
//
// POST /api/v1/vision/analyze
// 鉴权: Bearer API Key
func (c *Client) AnalyzeVision(ctx context.Context, req VisionRequest) (*VisionResponse, error) {
	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}

	var resp VisionResponse
	_, _, err := c.doRequest(ctx, "POST", "/api/v1/vision/analyze", req, "bearer", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// CompareVision 多图对比
func (c *Client) CompareVision(ctx context.Context, req VisionCompareRequest) (*VisionCompareResponse, error) {
	if req.Model == "" {
		req.Model = "qwen-vl-max"
	}

	var resp VisionCompareResponse
	_, _, err := c.doRequest(ctx, "POST", "/api/v1/vision/compare", req, "bearer", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// AnalyzeVideo 视频帧分析
func (c *Client) AnalyzeVideo(ctx context.Context, videoURL, prompt, model string, frameInterval int) (*VisionResponse, error) {
	if model == "" {
		model = "qwen-vl-max"
	}

	req := struct {
		VideoURL      string `json:"videoUrl"`
		Prompt        string `json:"prompt"`
		Model         string `json:"model"`
		FrameInterval int    `json:"frameInterval"`
		MaxTokens     int    `json:"maxTokens"`
	}{
		VideoURL:      videoURL,
		Prompt:        prompt,
		Model:         model,
		FrameInterval: frameInterval,
		MaxTokens:     1024,
	}

	var resp VisionResponse
	_, _, err := c.doRequest(ctx, "POST", "/api/v1/vision/video", req, "bearer", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// =============================================================================
// Face Detection API（v1：detect + 5 点 keypoints，无识别/无 PII）
// =============================================================================

// DetectFaceRequest 人脸检测请求（v1：detect only，无 profile）。
type DetectFaceRequest struct {
	DeviceID  string `json:"-"`
	ImageData []byte `json:"-"`
	FrameCRC  uint32 `json:"frameCrc"`
	TsMs      int64  `json:"tsMs"`
}

// Keypoint 5 点关键点（与 aisaas 服务端对齐）
type Keypoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// FaceDetection 人脸检测结果（v1：detect + 5 点 keypoints，无识别字段）。
type FaceDetection struct {
	FaceID      int64       `json:"faceId"`
	BoundingBox [4]int      `json:"boundingBox"`
	Confidence  float64     `json:"confidence"`
	Keypoints   [5]Keypoint `json:"keypoints"`
}

// DetectFaceResponse 人脸检测响应。
type DetectFaceResponse struct {
	Detections []FaceDetection `json:"detections"`
	LatencyMs  int             `json:"latencyMs"`
	ModelUsed  string          `json:"modelUsed"`
	Hit        bool            `json:"hit"`
}

// DetectFace 人脸检测（POST /internal/xiaozhi/v1/face/detect）。
//
// v1 范围：仅返回 bounding box + confidence + 5 点关键点，**不**返回 profile_id / display_name / 任何 PII。
func (c *Client) DetectFace(ctx context.Context, req DetectFaceRequest) (*DetectFaceResponse, error) {
	url := "/internal/xiaozhi/v1/face/detect"

	// 构建 multipart 请求
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("deviceId", req.DeviceID); err != nil {
		return nil, fmt.Errorf("write deviceId field: %w", err)
	}
	if err := w.WriteField("frameCrc", fmt.Sprintf("%d", req.FrameCRC)); err != nil {
		return nil, fmt.Errorf("write frameCrc field: %w", err)
	}
	if err := w.WriteField("tsMs", fmt.Sprintf("%d", req.TsMs)); err != nil {
		return nil, fmt.Errorf("write tsMs field: %w", err)
	}

	part, err := w.CreateFormFile("image", "frame.jpg")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(req.ImageData); err != nil {
		return nil, fmt.Errorf("write image data: %w", err)
	}
	w.Close()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+url, &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", w.FormDataContentType())
	httpReq.Header.Set("X-Internal-Token", c.cfg.InternalToken)
	httpReq.Header.Set("X-Device-Id", req.DeviceID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("detect face failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result DetectFaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
