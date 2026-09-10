# Integration Tests — E2E Smoke (T12)

End-to-end smoke tests for xiaozhi-server-go, covering the complete device lifecycle.

## Coverage

| Test | Scenario | Key Assertions |
|------|----------|----------------|
| `TestDeviceStartup_FirstBoot_RequestAPIKey` | Device first boot, no key file | `RegisterDevice` called, key file written, `GetAPIKey()` returns non-empty |
| `TestDeviceStartup_Reboot_LoadExistingKey` | Device reboot with existing key file | `RegisterDevice` NOT called, `VerifyKey` called, same key loaded |
| `TestDeviceStartup_VerifyFail_ForceRotate` | API Key 401 on verify | `RotateKey` called, new key written, device remains registered |
| `TestWSConnection_HelloHandshake` | WebSocket hello → welcome | Welcome `session_id` non-empty, `ListPersonas` called |
| `TestWSConnection_ListenStream` | Listen start/stop + audio frames | ASR, LLM, TTS endpoints all called |
| `TestWSConnection_ListenStream_RealHandler` | Full pipeline with real transport.Handler | End-to-end verification with async processing |

## Running

```bash
# Run all integration tests
go test -tags=integration ./integration/...

# Run with verbose output
go test -tags=integration -v ./integration/...

# Run only device startup tests
go test -tags=integration -run "TestDeviceStartup" ./integration/...

# Run only WebSocket tests
go test -tags=integration -run "TestWS" ./integration/...
```

## Architecture

- **Mock Aisaas** (`mock_aisaas.go`): `httptest.NewServer` based mock. Records all
  incoming requests in a `CallLog` for assertion. Configured via `SetResponse` /
  `SetResponseFunc`.
- **Test Isolation**: Each test uses `t.TempDir()` for key storage. No shared state.
- **Real Components**: `device.Manager`, `aisaas.Client`, `transport.Handler`,
  `server.NewOnHello/OnListen/...` are real — only the aisaas HTTP endpoint is mocked.

## Constraints

- No hard sleeps — use channel-based reads with timeout instead
- No external Redis required — rate limiting is disabled in test config
- Opus codec skipped — fake PCM bytes are sent, mock returns pre-computed audio
