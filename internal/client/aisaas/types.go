// Package aisaas 实现 ykt-aisaas V2 客户端 SDK。
//
// 覆盖 17+ 个接口：鉴权、Chat、TTS、ASR、Memory、Persona、Session、API Key 管理。
// 所有数据结构对齐 docs/openapi/aisaas-v2.yaml。
package aisaas

import "time"

// ============================================================
// 通用响应信封（SaaS 自有接口）
// ============================================================

// APIResponse 统一响应信封。
// SaaS 自有接口（/api/v1/*）和内部接口（/internal/api/v1/*）使用此格式。
type APIResponse struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	RequestID string      `json:"requestId,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// APIError OpenAI 兼容接口错误响应。
type APIError struct {
	Error     APIErrorDetail `json:"error"`
	RequestID string         `json:"requestId,omitempty"`
}

// APIErrorDetail 错误详情。
type APIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ============================================================
// Auth 模块
// ============================================================

// DeviceCredential 设备凭证。
type DeviceCredential struct {
	DeviceID  string    `json:"deviceId"`
	APIKey    string    `json:"apiKey"`
	KeyID     int64     `json:"keyId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// DeviceHWInfo 设备硬件信息。
type DeviceHWInfo struct {
	MAC             string `json:"mac,omitempty"`
	ChipType        string `json:"chipType,omitempty"`
	FirmwareVersion string `json:"firmwareVersion,omitempty"`
	EfuseMAC        string `json:"efuseMAC,omitempty"`
}

// registerDeviceRequest 注册设备请求体。
type registerDeviceRequest struct {
	HwInfo DeviceHWInfo `json:"hwInfo"`
}

// registerDeviceResponse 注册设备响应体。
type registerDeviceResponse struct {
	DeviceID  string `json:"deviceId"`
	APIKey    string `json:"apiKey"`
	KeyID     int64  `json:"keyId"`
	ExpiresAt string `json:"expiresAt"`
}

// ============================================================
// API Key 模块
// ============================================================

// RotateKeyRequest API Key 轮换请求。
type RotateKeyRequest struct {
	ExpireDays     int    `json:"expireDays"`
	RotateStrategy string `json:"rotateStrategy,omitempty"` // time_24h, on_use_count, manual
}

// RotateKeyResponse API Key 轮换响应。
type RotateKeyResponse struct {
	OldKeyID        int64  `json:"oldKeyId"`
	OldKeyExpiresAt string `json:"oldKeyExpiresAt"`
	NewKeyID        int64  `json:"newKeyId"`
	NewAPIKey       string `json:"newApiKey"`
	NewKeyPrefix    string `json:"newKeyPrefix"`
	NewKeyExpiresAt string `json:"newKeyExpiresAt"`
}

// RevokeKeyRequest API Key 撤销请求。
type RevokeKeyRequest struct {
	Reason string `json:"reason,omitempty"`
}

// RevokeKeyResponse API Key 撤销响应。
type RevokeKeyResponse struct {
	KeyID     int64  `json:"keyId"`
	RevokedAt string `json:"revokedAt"`
	Reason    string `json:"reason,omitempty"`
}

// ============================================================
// Chat 模块
// ============================================================

// ChatRequest 对话请求（OpenAI 兼容）。
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	// 扩展参数
	XToolsMCP         bool    `json:"x_tools_mcp,omitempty"`
	XKnowledgeBaseIDs []int64 `json:"x_knowledge_base_ids,omitempty"`
}

// ChatMessage 对话消息。
type ChatMessage struct {
	Role    string `json:"role"`    // system, user, assistant
	Content string `json:"content"` // 消息内容
}

// ChatResponse 非流式对话响应。
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
}

// ChatChoice 对话选项。
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatUsage Token 用量。
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatStreamChunk 流式对话块。
type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatStreamChoice `json:"choices"`
	Usage   *ChatUsage         `json:"usage,omitempty"`
}

// ChatStreamChoice 流式对话选项。
type ChatStreamChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

// ChatDelta 流式增量。
type ChatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ============================================================
// TTS 模块
// ============================================================

// TTSRequest TTS 请求。
type TTSRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"` // mp3, opus, aac, flac, wav, pcm
	Speed          float64 `json:"speed,omitempty"`
}

// ============================================================
// ASR 模块
// ============================================================

// ASRResponse ASR 响应。
type ASRResponse struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration,omitempty"`
	Language string  `json:"language,omitempty"`
}

// ============================================================
// Memory 模块
// ============================================================

// WriteMemoryMessageRequest 写入短期消息请求。
type WriteMemoryMessageRequest struct {
	SessionID string                 `json:"sessionId,omitempty"`
	Role      string                 `json:"role"`    // user, assistant, system
	Content   string                 `json:"content"` // 消息内容
	Metadata  *MemoryMessageMetadata `json:"metadata,omitempty"`
}

// MemoryMessageMetadata 消息元数据。
type MemoryMessageMetadata struct {
	Model     string `json:"model,omitempty"`
	Tokens    int    `json:"tokens,omitempty"`
	LatencyMs int    `json:"latencyMs,omitempty"`
}

// WriteMemoryMessageResponse 写入消息响应。
type WriteMemoryMessageResponse struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	CreatedAt string `json:"createdAt"`
}

// ListMemoryMessagesResponse 消息列表响应。
type ListMemoryMessagesResponse struct {
	Items      []MemoryMessage `json:"items"`
	HasMore    bool            `json:"hasMore"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

// MemoryMessage 记忆消息。
type MemoryMessage struct {
	ID        int64                  `json:"id"`
	SessionID string                 `json:"sessionId"`
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Metadata  *MemoryMessageMetadata `json:"metadata,omitempty"`
	CreatedAt string                 `json:"createdAt"`
}

// MemoryGraphResponse 长期记忆图谱响应。
type MemoryGraphResponse struct {
	DeviceID    string             `json:"deviceId"`
	Entities    []MemoryEntity     `json:"entities"`
	Preferences []MemoryPreference `json:"preferences"`
	Events      []MemoryEvent      `json:"events"`
	UpdatedAt   string             `json:"updatedAt"`
}

// MemoryEntity 记忆实体。
type MemoryEntity struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"` // person, place, object, organization, concept
	Name       string                 `json:"name"`
	Confidence float64                `json:"confidence"`
	Mentions   []string               `json:"mentions,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// MemoryPreference 记忆偏好。
type MemoryPreference struct {
	Dimension      string  `json:"dimension"`
	Value          string  `json:"value"`
	Confidence     float64 `json:"confidence"`
	SourceMessages []int64 `json:"sourceMessages,omitempty"`
}

// MemoryEvent 记忆事件。
type MemoryEvent struct {
	ID           string                 `json:"id,omitempty"`
	EventType    string                 `json:"eventType"` // appointment, task, reminder, interaction, custom
	EventTime    string                 `json:"eventTime"`
	Description  string                 `json:"description"`
	Participants []string               `json:"participants,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ExtractMemoryRequest 抽取实体请求。
type ExtractMemoryRequest struct {
	SessionID    string                `json:"sessionId,omitempty"`
	Messages     []ExtractMessageInput `json:"messages"`
	ExtractTypes []string              `json:"extractTypes"` // entity, preference, event, skill
}

// ExtractMessageInput 抽取消息输入。
type ExtractMessageInput struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ExtractMemoryResponse 抽取任务响应。
type ExtractMemoryResponse struct {
	TaskID            string `json:"taskId"`
	Status            string `json:"status"` // pending, running, completed, failed
	EstimatedEntities int    `json:"estimatedEntities"`
	CreatedAt         string `json:"createdAt,omitempty"`
}

// SummarizeMemoryRequest 摘要聚合请求。
type SummarizeMemoryRequest struct {
	SessionID      string   `json:"sessionId,omitempty"`
	SummarizeTypes []string `json:"summarizeTypes,omitempty"` // conversation, topic, key_event, personality
	TopicFocus     string   `json:"topicFocus,omitempty"`
}

// SummarizeMemoryResponse 摘要任务响应。
type SummarizeMemoryResponse struct {
	TaskID    string `json:"taskId"`
	Status    string `json:"status"` // pending, running, completed, failed
	CreatedAt string `json:"createdAt,omitempty"`
}

// ============================================================
// Persona 模块
// ============================================================

// Persona 人设配置。
type Persona struct {
	ID           int64                  `json:"id"`
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	SystemPrompt string                 `json:"systemPrompt,omitempty"`
	Skills       []PersonaSkill         `json:"skills,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    string                 `json:"createdAt"`
	UpdatedAt    string                 `json:"updatedAt,omitempty"`
	// 扩展字段（设计规格 1.2.2）
	PersonalityTraits       map[string]interface{} `json:"personalityTraits,omitempty"`
	VoicePreference         string                 `json:"voicePreference,omitempty"`
	RelationshipStages      map[string]interface{} `json:"relationshipStages,omitempty"`
	DefaultModelID          string                 `json:"defaultModelId,omitempty"`
	DefaultKnowledgeBaseIDs []int64                `json:"defaultKnowledgeBaseIds,omitempty"`
	Temperature             float64                `json:"temperature,omitempty"`
	TopP                    float64                `json:"topP,omitempty"`
	MaxTokens               int                    `json:"maxTokens,omitempty"`
}

// PersonaSkill 人设技能。
type PersonaSkill struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Type   string                 `json:"type"` // tool, knowledge, action
	Config map[string]interface{} `json:"config,omitempty"`
}

// PersonaBind Persona 绑定信息。
type PersonaBind struct {
	BindID    int64  `json:"bindId,omitempty"`
	PersonaID int64  `json:"personaId"`
	DeviceID  string `json:"deviceId"`
	IsDefault bool   `json:"isDefault"`
	BoundAt   string `json:"boundAt,omitempty"`
}

// PersonaWithBind 带绑定信息的 Persona。
type PersonaWithBind struct {
	Persona
	PersonaBind *PersonaBind `json:"persona_bind,omitempty"`
}

// ListPersonasResponse Persona 列表响应。
type ListPersonasResponse struct {
	Items   []PersonaWithBind `json:"items"`
	HasMore bool              `json:"hasMore,omitempty"`
}

// ============================================================
// Session 模块
// ============================================================

// CreateSessionRequest 创建会话请求。
type CreateSessionRequest struct {
	DeviceID     string           `json:"deviceId,omitempty"`
	Dimension    string           `json:"dimension"`    // llm_tokens_in, llm_tokens_out, tts_chars, asr_seconds
	QuotaInitial int64            `json:"quotaInitial"` // 申请借记量
	PersonaID    *int64           `json:"personaId,omitempty"`
	Metadata     *SessionMetadata `json:"metadata,omitempty"`
}

// SessionMetadata 会话元数据。
type SessionMetadata struct {
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
	Dimension string `json:"dimension,omitempty"`
}

// CreateSessionResponse 创建会话响应。
type CreateSessionResponse struct {
	SessionID      string        `json:"sessionId"`
	DeviceID       string        `json:"deviceId"`
	Dimension      string        `json:"dimension"`
	QuotaInitial   int64         `json:"quotaInitial"`
	QuotaRemaining int64         `json:"quotaRemaining"`
	QuotaSnapshot  QuotaSnapshot `json:"quotaSnapshot"`
	Persona        *Persona      `json:"persona,omitempty"`
	CreatedAt      string        `json:"createdAt"`
}

// QuotaSnapshot 配额快照。
type QuotaSnapshot struct {
	TenantID             int64  `json:"tenantId"`
	DeviceQuotaLimit     int64  `json:"deviceQuotaLimit"`
	DeviceQuotaUsed      int64  `json:"deviceQuotaUsed"`
	DeviceQuotaRemaining int64  `json:"deviceQuotaRemaining"`
	SnapshotTime         string `json:"snapshotTime"`
}

// SessionHistoryResponse 会话历史响应。
type SessionHistoryResponse struct {
	SessionID  string           `json:"sessionId"`
	Items      []SessionMessage `json:"items"`
	HasMore    bool             `json:"hasMore"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// SessionMessage 会话消息。
type SessionMessage struct {
	ID        int64                  `json:"id"`
	Role      string                 `json:"role"`
	Content   string                 `json:"content"`
	Metadata  *MemoryMessageMetadata `json:"metadata,omitempty"`
	CreatedAt string                 `json:"createdAt"`
}

// EndSessionRequest 结束会话请求。
type EndSessionRequest struct {
	ActualCost QuotaUsage `json:"actualCost"`
	Status     string     `json:"status"` // success, failed
}

// EndSessionResponse 结束会话响应。
type EndSessionResponse struct {
	SessionID     string     `json:"sessionId"`
	QuotaUsed     QuotaUsage `json:"quotaUsed"`
	QuotaRefunded QuotaUsage `json:"quotaRefunded"`
	EndedAt       string     `json:"endedAt"`
}

// QuotaUsage 配额使用量（多维度）。
type QuotaUsage struct {
	LLMTokensIn  int64 `json:"llm_tokens_in,omitempty"`
	LLMTokensOut int64 `json:"llm_tokens_out,omitempty"`
	TTSChars     int64 `json:"tts_chars,omitempty"`
	ASRSeconds   int64 `json:"asr_seconds,omitempty"`
}
