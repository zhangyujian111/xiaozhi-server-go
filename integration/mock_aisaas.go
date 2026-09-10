//go:build integration
// +build integration

// Package integration provides end-to-end smoke tests for xiaozhi-server-go.
//
// Tests verify the complete链路：device startup + API Key apply + Hello handshake + Listen stream.
// Mock aisaas server is used to avoid external dependencies.
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// CallLog — records all incoming requests for assertion
// =============================================================================

// Call records a single HTTP request.
type Call struct {
	Method  string
	Path    string
	Headers http.Header
	Body    string
}

// CallLog records all requests received by the mock server.
type CallLog struct {
	mu    sync.RWMutex
	calls []Call
}

func (l *CallLog) record(method, path string, headers http.Header) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, Call{
		Method:  method,
		Path:    path,
		Headers: headers.Clone(),
	})
}

// Count returns the number of calls matching the given method and path prefix.
func (l *CallLog) Count(method, pathPrefix string) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	n := 0
	for _, c := range l.calls {
		if c.Method == method && strings.HasPrefix(c.Path, pathPrefix) {
			n++
		}
	}
	return n
}

// GetCalls returns all recorded calls.
func (l *CallLog) GetCalls() []Call {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]Call(nil), l.calls...)
}

// Contains returns true if there is at least one call matching method and path.
func (l *CallLog) Contains(method, pathPrefix string) bool {
	return l.Count(method, pathPrefix) > 0
}

// Reset clears all recorded calls.
func (l *CallLog) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = nil
}

// =============================================================================
// MockResponse — response configuration
// =============================================================================

// MockResponse defines the response for a mocked endpoint.
type MockResponse struct {
	StatusCode int
	Body       string
	Delay      time.Duration
}

// MockAisaas provides a test mock for the ykt-aisaas API.
type MockAisaas struct {
	server    *httptest.Server
	mux       *http.ServeMux
	calls     *CallLog
	responses map[string]MockResponse
	mu        sync.RWMutex
}

// NewMockAisaas creates a mock aisaas server.
func NewMockAisaas(t *testing.T) *MockAisaas {
	m := &MockAisaas{
		calls:     &CallLog{},
		responses: make(map[string]MockResponse),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", m.handleAll)
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

// URL returns the base URL of the mock server.
func (m *MockAisaas) URL() string {
	return m.server.URL
}

// SetResponse configures a fixed response for a request pattern (METHOD PATH).
func (m *MockAisaas) SetResponse(method, path string, resp MockResponse) {
	key := method + " " + path
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[key] = resp
}

// SetResponseFunc registers a handler function for a specific path pattern.
func (m *MockAisaas) SetResponseFunc(pattern string, handler func(w http.ResponseWriter, r *http.Request)) {
	m.mux.Handle(pattern, http.HandlerFunc(handler))
}

// Calls returns the call log.
func (m *MockAisaas) Calls() *CallLog {
	return m.calls
}

// Reset clears all recorded calls and responses.
func (m *MockAisaas) Reset() {
	m.calls.Reset()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = make(map[string]MockResponse)
}

// handleAll handles all incoming requests.
func (m *MockAisaas) handleAll(w http.ResponseWriter, r *http.Request) {
	// Record the call (body is read to drain the request body for keep-alive)
	_, _ = io.ReadAll(r.Body)
	r.Body.Close()
	m.calls.record(r.Method, r.URL.Path, r.Header)

	// Determine response key
	key := r.Method + " " + r.URL.Path
	m.mu.RLock()
	resp, ok := m.responses[key]
	m.mu.RUnlock()

	if !ok {
		// Check if there's a pattern match
		m.mu.RLock()
		for k, v := range m.responses {
			if strings.HasPrefix(key, k) {
				resp = v
				ok = true
				break
			}
		}
		m.mu.RUnlock()
	}

	if ok {
		if resp.Delay > 0 {
			time.Sleep(resp.Delay)
		}
		w.WriteHeader(resp.StatusCode)
		w.Write([]byte(resp.Body))
		return
	}

	// No response configured — return 404 to make missing mocks obvious
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(fmt.Sprintf(`{"error": "mock not configured for %s %s"}`, r.Method, r.URL.Path)))
}

// =============================================================================
// Mock Response Factories
// =============================================================================

// mockRegisterDeviceResponse is the response for POST /internal/api/v1/devices/{deviceId}/register.
func mockRegisterDeviceResponse(deviceID string) string {
	return fmt.Sprintf(`{
		"deviceId": %q,
		"apiKey": "sk-test-api-key-%s-xxxx",
		"keyId": 12345,
		"expiresAt": "2027-01-01T00:00:00Z"
	}`, deviceID, deviceID)
}

// mockVerifyKeyResponse returns a valid 200 response for GET /v1/models.
func mockVerifyKeyResponse() string {
	return `{}`
}

// mockVerifyKeyError returns a 401 response for GET /v1/models.
func mockVerifyKeyError() string {
	return `{"error": {"message": "invalid api key", "type": "aisaas_error", "code": "40101"}}`
}

// mockRotateKeyResponse returns a successful rotation response.
func mockRotateKeyResponse(oldKeyID int64) string {
	return fmt.Sprintf(`{
		"oldKeyId": %d,
		"oldKeyExpiresAt": "2026-09-02T12:05:00Z",
		"newKeyId": 12346,
		"newApiKey": "sk-rotated-key-xxxx",
		"newKeyPrefix": "sk-rotated-key-xxxx",
		"newKeyExpiresAt": "2027-01-01T00:00:00Z"
	}`, oldKeyID)
}

// mockListPersonasResponse returns a persona list.
func mockListPersonasResponse() string {
	return `{
		"items": [
			{
				"id": 1,
				"name": "小智助手",
				"version": "1.0",
				"systemPrompt": "你是一个智能助手。",
				"skills": [],
				"metadata": {},
				"createdAt": "2026-01-01T00:00:00Z"
			}
		],
		"hasMore": false
	}`
}

// mockCreateSessionResponse returns a session creation response.
func mockCreateSessionResponse(deviceID string) string {
	return fmt.Sprintf(`{
		"sessionId": "sess-%s-001",
		"deviceId": %q,
		"dimension": "llm_tokens_in",
		"quotaInitial": 2048,
		"quotaRemaining": 2048,
		"quotaSnapshot": {
			"tenantId": 1,
			"deviceQuotaLimit": 100000,
			"deviceQuotaUsed": 0,
			"deviceQuotaRemaining": 100000,
			"snapshotTime": "2026-09-02T00:00:00Z"
		},
		"createdAt": "2026-09-02T00:00:00Z"
	}`, deviceID, deviceID)
}

// mockEndSessionResponse returns a session end response.
func mockEndSessionResponse(sessionID string) string {
	return fmt.Sprintf(`{
		"sessionId": %q,
		"quotaUsed": {"llm_tokens_in": 100, "llm_tokens_out": 50},
		"quotaRefunded": {"llm_tokens_in": 1948, "llm_tokens_out": 0},
		"endedAt": "2026-09-02T00:01:00Z"
	}`, sessionID)
}

// mockTranscriptionResponse returns an ASR transcription response.
func mockTranscriptionResponse(text string) string {
	return fmt.Sprintf(`{"text": %q, "duration": 1.5, "language": "zh"}`, text)
}

// mockChatCompletionResponse returns an LLM chat completion response.
func mockChatCompletionResponse(content string) string {
	return fmt.Sprintf(`{
		"id": "chat-001",
		"object": "chat.completion",
		"created": 1725222000,
		"model": "gpt-4o",
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": %q},
				"finish_reason": "stop"
			}
		],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`, content)
}

// mockAudioSpeechResponse returns fake audio bytes for TTS.
func mockAudioSpeechResponse() []byte {
	// Return a minimal valid WAV header + short silent audio for testing
	header := make([]byte, 44)
	// RIFF header
	copy(header[0:4], []byte("RIFF"))
	// File size (little endian)
	header[4] = 36
	header[5] = 0
	header[6] = 0
	header[7] = 0
	// WAVE
	copy(header[8:12], []byte("WAVE"))
	// fmt
	copy(header[12:16], []byte("fmt "))
	// Subchunk1Size (16 for PCM)
	header[16] = 16
	header[17] = 0
	header[18] = 0
	header[19] = 0
	// AudioFormat (1 for PCM)
	header[20] = 1
	header[21] = 0
	// NumChannels (1)
	header[22] = 1
	header[23] = 0
	// SampleRate (16000)
	header[24] = 0x40
	header[25] = 0x1F
	header[26] = 0
	header[27] = 0
	// ByteRate (32000)
	header[28] = 0x80
	header[29] = 0x7D
	header[30] = 0
	header[31] = 0
	// BlockAlign (2)
	header[32] = 2
	header[33] = 0
	// BitsPerSample (16)
	header[34] = 0x10
	header[35] = 0
	// data
	copy(header[36:40], []byte("data"))
	// Subchunk2Size
	header[40] = 0
	header[41] = 0
	header[42] = 0
	header[43] = 0

	// Silent audio samples (80 bytes of silence)
	silent := make([]byte, 80)
	return append(header, silent...)
}

// =============================================================================
// Convenience Setup Helpers
// =============================================================================

// SetupHappyPath configures the mock for the full happy-path scenario:
// Register → Verify (200) → ListPersonas → CreateSession → Transcription → ChatCompletion → EndSession.
func SetupHappyPath(m *MockAisaas, deviceID string) {
	// Device registration (internal endpoint)
	m.SetResponse("POST", "/internal/api/v1/devices/"+deviceID+"/register", MockResponse{
		StatusCode: 200,
		Body:       mockRegisterDeviceResponse(deviceID),
	})

	// API Key verification (OpenAI-compatible endpoint)
	m.SetResponse("GET", "/v1/models", MockResponse{
		StatusCode: 200,
		Body:       mockVerifyKeyResponse(),
	})

	// List personas (called during Hello handshake)
	m.SetResponse("GET", "/api/v1/personas", MockResponse{
		StatusCode: 200,
		Body:       mockListPersonasResponse(),
	})

	// Create session
	m.SetResponse("POST", "/api/v1/sessions/"+deviceID, MockResponse{
		StatusCode: 200,
		Body:       mockCreateSessionResponse(deviceID),
	})

	// ASR transcription
	m.SetResponse("POST", "/v1/audio/transcriptions", MockResponse{
		StatusCode: 200,
		Body:       mockTranscriptionResponse("hello"),
	})

	// LLM chat completion
	m.SetResponse("POST", "/v1/chat/completions", MockResponse{
		StatusCode: 200,
		Body:       mockChatCompletionResponse("world"),
	})

	// TTS speech
	m.SetResponse("POST", "/v1/audio/speech", MockResponse{
		StatusCode: 200,
		Body:       string(mockAudioSpeechResponse()),
	})

	// End session
	m.SetResponse("POST", "/api/v1/sessions/sess-"+deviceID+"-001/end", MockResponse{
		StatusCode: 200,
		Body:       mockEndSessionResponse("sess-" + deviceID + "-001"),
	})
}

// SetupVerifyFail configures the mock for a verification failure scenario:
// Verify (401) → Rotate → Verify (200).
func SetupVerifyFail(m *MockAisaas, deviceID string, oldKeyID int64) {
	m.SetResponse("GET", "/v1/models", MockResponse{
		StatusCode: 401,
		Body:       mockVerifyKeyError(),
	})

	m.SetResponse("POST", fmt.Sprintf("/internal/api/v1/apikeys/%d/rotate", oldKeyID), MockResponse{
		StatusCode: 200,
		Body:       mockRotateKeyResponse(oldKeyID),
	})

	// After rotation, verify should pass
	m.SetResponse("GET", "/v1/models", MockResponse{
		StatusCode: 200,
		Body:       mockVerifyKeyResponse(),
	})
}

// SetupReboot configures the mock for a reboot scenario:
// Verify (200) only, no register.
func SetupReboot(m *MockAisaas) {
	m.SetResponse("GET", "/v1/models", MockResponse{
		StatusCode: 200,
		Body:       mockVerifyKeyResponse(),
	})
}

// JSON RPC helpers -----------------------------------------------------------

// JSONRPCRequest builds a JSON-RPC request message.
func JSONRPCRequest(id int64, method string, params map[string]interface{}) string {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	return string(b)
}

// JSONRPCNotification builds a JSON-RPC notification (no id).
func JSONRPCNotification(method string, params map[string]interface{}) string {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	return string(b)
}
