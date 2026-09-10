<template>
  <div class="ws-console">
    <el-row :gutter="20">
      <el-col :span="14">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>JSON-RPC 控制台</span>
              <div>
                <el-tag :type="wsState.type" size="small">{{ wsState.label }}</el-tag>
              </div>
            </div>
          </template>

          <el-form :inline="true" size="small">
            <el-form-item label="deviceId">
              <el-input v-model="deviceId" placeholder="ESP32-001" style="width: 160px" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="connect" :disabled="connected">连接</el-button>
              <el-button type="danger" @click="disconnect" :disabled="!connected">断开</el-button>
            </el-form-item>
          </el-form>

          <el-divider />

          <el-form label-width="80px" size="small">
            <el-form-item label="方法">
              <el-select v-model="selectedMethod" style="width: 100%">
                <el-option label="hello (握手)" value="hello" />
                <el-option label="listen start (开始录音)" value="listen_start" />
                <el-option label="listen stop (结束录音)" value="listen_stop" />
                <el-option label="abort (中止)" value="abort" />
                <el-option label="state (状态上报)" value="state" />
                <el-option label="ping (心跳)" value="ping" />
                <el-option label="goodbye (告别)" value="goodbye" />
                <el-option label="自定义 JSON" value="custom" />
              </el-select>
            </el-form-item>

            <el-form-item label="payload" v-if="selectedMethod === 'custom'">
              <el-input
                v-model="customPayload"
                type="textarea"
                :rows="4"
                placeholder='{"type":"hello", ...}'
              />
            </el-form-item>

            <el-form-item v-else-if="selectedMethod === 'state'" label="payload">
              <el-input
                v-model="statePayload"
                type="textarea"
                :rows="3"
              />
            </el-form-item>

            <el-form-item v-else-if="selectedMethod === 'hello'" label="payload">
              <el-input
                v-model="helloPayload"
                type="textarea"
                :rows="6"
              />
            </el-form-item>

            <el-form-item>
              <el-button type="primary" @click="sendRpc" :disabled="!connected || !selectedMethod">发送</el-button>
              <el-button @click="clearLogs">清空日志</el-button>
            </el-form-item>
          </el-form>

          <el-divider />

          <div class="log-header">
            <span>通信日志 ({{ logs.length }})</span>
            <el-checkbox v-model="autoScroll" size="small">自动滚动</el-checkbox>
          </div>
          <div ref="logBoxRef" class="log-box">
            <div v-for="(log, i) in logs" :key="i" :class="['log-item', `log-${log.dir}`]">
              <span class="log-time">{{ formatTime(log.time) }}</span>
              <span class="log-tag">{{ log.dir === 'send' ? 'SEND' : log.dir === 'recv' ? 'RECV' : log.dir === 'sys' ? 'SYS' : 'BIN' }}</span>
              <pre class="log-content">{{ log.text }}</pre>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :span="10">
        <el-card>
          <template #header>
            <span>二进制音频帧（Opus/PCM）</span>
          </template>

          <el-form label-width="80px" size="small">
            <el-form-item label="帧类型">
              <el-select v-model="frameType" style="width: 100%">
                <el-option label="0x00 Opus" :value="0" />
                <el-option label="0x01 PCM" :value="1" />
                <el-option label="0x02 Silero VAD" :value="2" />
              </el-select>
            </el-form-item>
            <el-form-item label="音频源">
              <el-radio-group v-model="audioSource">
                <el-radio-button label="file">本地文件</el-radio-button>
                <el-radio-button label="mock">Mock 数据</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item v-if="audioSource === 'file'" label="文件">
              <input type="file" @change="onFile" />
              <div v-if="audioFile" class="file-info">
                {{ audioFile.name }} ({{ audioFile.size }} bytes)
              </div>
            </el-form-item>
            <el-form-item v-if="audioSource === 'mock'" label="字节数">
              <el-input-number v-model="mockSize" :min="8" :max="65536" :step="8" />
            </el-form-item>
            <el-form-item>
              <el-button type="warning" @click="sendBinary" :disabled="!connected || !listenStarted">发送一帧</el-button>
              <el-button type="info" @click="sendBinaryLoop" :disabled="!connected || !listenStarted">连续发送 (60fps × N 帧)</el-button>
            </el-form-item>
            <el-form-item label="帧数" v-if="audioSource === 'mock' || (audioSource === 'file' && audioFile)">
              <el-input-number v-model="loopCount" :min="1" :max="1000" />
            </el-form-item>
          </el-form>

          <el-divider />

          <el-alert
            title="帧格式：[4 字节大端 frame_length(含 5 字节头)] [1 字节 frame_type] [N 字节 data]"
            type="info"
            :closable="false"
            show-icon
          />
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 20px">
      <template #header>
        <span>收到的 TTS/STT/LLM 流（点击展开）</span>
      </template>
      <el-table :data="streamRows" stripe size="small" :empty-text="'尚无下行流'">
        <el-table-column prop="type" label="type" width="120" />
        <el-table-column prop="time" label="时间" width="180" />
        <el-table-column prop="text" label="内容" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick } from 'vue'
import { ElMessage } from 'element-plus'

interface LogItem {
  time: number
  dir: 'send' | 'recv' | 'sys' | 'binary'
  text: string
}

const deviceId = ref('ESP32-001')

const wsState = reactive({ type: 'info' as 'success' | 'info' | 'danger' | 'warning', label: '未连接' })
const connected = computed(() => wsState.label === '已连接')

let ws: WebSocket | null = null

const selectedMethod = ref('hello')
const customPayload = ref('')
const helloPayload = ref(JSON.stringify({
  type: 'hello',
  version: 1,
  mac_address: 'aa:bb:cc:dd:ee:01',
  device_id: 'ESP32-001',
  app_version: '1.0.0',
  chip_model: 'esp32-s3'
}, null, 2))
const statePayload = ref(JSON.stringify({
  type: 'state',
  battery: 80,
  charging: false,
  volume: 60,
  wifi_rssi: -55,
  uptime: 1234,
  free_heap: 200000
}, null, 2))

const logs = ref<LogItem[]>([])
const autoScroll = ref(true)
const logBoxRef = ref<HTMLElement | null>(null)

function pushLog(dir: LogItem['dir'], text: string) {
  logs.value.push({ time: Date.now(), dir, text })
  if (logs.value.length > 500) logs.value.shift()
  if (autoScroll.value) {
    nextTick(() => {
      if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight
    })
  }
}

function formatTime(t: number) {
  const d = new Date(t)
  return d.toLocaleTimeString('zh-CN')
}

function clearLogs() {
  logs.value = []
}

const streamRows = ref<{ type: string; time: string; text: string }[]>([])

function connect() {
  if (!deviceId.value) {
    ElMessage.warning('请输入 deviceId')
    return
  }
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const url = `${proto}://${location.host}/ws/${encodeURIComponent(deviceId.value)}`

  pushLog('sys', `connecting to ${url}`)
  ws = new WebSocket(url)

  ws.onopen = () => {
    wsState.type = 'success'
    wsState.label = '已连接'
    pushLog('sys', 'WebSocket connected')
  }

  ws.onmessage = (ev) => {
    if (typeof ev.data === 'string') {
      try {
        const obj = JSON.parse(ev.data)
        pushLog('recv', JSON.stringify(obj, null, 2))
        if (obj.type === 'tts' || obj.type === 'stt' || obj.type === 'llm' || obj.type === 'text' || obj.type === 'mcp_result') {
          streamRows.value.push({
            type: obj.type,
            time: new Date().toLocaleTimeString('zh-CN'),
            text: obj.text || obj.content || JSON.stringify(obj)
          })
        }
      } catch {
        pushLog('recv', ev.data)
      }
    } else if (ev.data instanceof Blob) {
      ev.data.arrayBuffer().then((buf) => {
        const arr = new Uint8Array(buf)
        const header = arr.slice(0, 5)
        const type = header[4]
        pushLog('binary', `binary frame: len=${arr.length} type=0x${type.toString(16).padStart(2, '0')} data=${arr.length - 5}B`)
      })
    }
  }

  ws.onerror = (e) => {
    pushLog('sys', `WebSocket error`)
  }

  ws.onclose = (ev) => {
    wsState.type = 'danger'
    wsState.label = '未连接'
    pushLog('sys', `WebSocket closed: code=${ev.code} reason=${ev.reason}`)
    ws = null
    listenStarted.value = false
  }
}

function disconnect() {
  if (ws) {
    ws.close()
    ws = null
  }
}

const listenStarted = ref(false)

function sendRpc() {
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    ElMessage.warning('WebSocket 未连接')
    return
  }
  let payload: any
  try {
    switch (selectedMethod.value) {
      case 'hello':       payload = JSON.parse(helloPayload.value); break
      case 'state':       payload = JSON.parse(statePayload.value); break
      case 'listen_start':payload = { type: 'listen', state: 'start', mode: 'manual' }; break
      case 'listen_stop': payload = { type: 'listen', state: 'stop' }; break
      case 'abort':       payload = { type: 'abort', reason: 'user_abort' }; break
      case 'ping':        payload = { type: 'ping' }; break
      case 'goodbye':     payload = { type: 'goodbye', reason: 'user_bye' }; break
      case 'custom':      payload = JSON.parse(customPayload.value); break
      default: return
    }
  } catch (e: any) {
    ElMessage.error(`JSON 解析失败: ${e.message}`)
    return
  }
  if (selectedMethod.value === 'listen_start') listenStarted.value = true
  if (selectedMethod.value === 'listen_stop' || selectedMethod.value === 'abort') listenStarted.value = false
  const text = JSON.stringify(payload)
  ws.send(text)
  pushLog('send', text)
}

const frameType = ref(0)
const audioSource = ref<'file' | 'mock'>('mock')
const audioFile = ref<File | null>(null)
const mockSize = ref(120)
const loopCount = ref(10)

function onFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (f) audioFile.value = f
}

function buildFrame(data: Uint8Array, type: number): ArrayBuffer {
  const total = 5 + data.length
  const buf = new ArrayBuffer(total)
  const view = new DataView(buf)
  view.setUint32(0, total, false)
  view.setUint8(4, type)
  new Uint8Array(buf, 5).set(data)
  return buf
}

async function sendBinary() {
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    ElMessage.warning('WebSocket 未连接')
    return
  }
  let data: Uint8Array
  if (audioSource.value === 'file') {
    if (!audioFile.value) {
      ElMessage.warning('请先选文件')
      return
    }
    data = new Uint8Array(await audioFile.value.arrayBuffer())
  } else {
    data = new Uint8Array(mockSize.value).map(() => Math.floor(Math.random() * 256))
  }
  const frame = buildFrame(data, frameType.value)
  ws.send(frame)
  pushLog('binary', `sent frame: type=0x${frameType.value.toString(16).padStart(2, '0')} dataLen=${data.length}`)
}

async function sendBinaryLoop() {
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    ElMessage.warning('WebSocket 未连接')
    return
  }
  let baseData: Uint8Array
  if (audioSource.value === 'file' && audioFile.value) {
    baseData = new Uint8Array(await audioFile.value.arrayBuffer())
  } else {
    baseData = new Uint8Array(mockSize.value).map(() => Math.floor(Math.random() * 256))
  }
  const interval = setInterval(() => {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      clearInterval(interval)
      return
    }
    ws.send(buildFrame(baseData, frameType.value))
  }, 60)
  pushLog('sys', `开始连续发送：${loopCount.value} 帧 @ 60ms/帧`)
  let sent = 0
  const ticker = setInterval(() => {
    sent++
    if (sent >= loopCount.value) {
      clearInterval(ticker)
      clearInterval(interval)
      pushLog('sys', `连续发送完成：${loopCount.value} 帧`)
    }
  }, 60)
}
</script>

<style scoped>
.ws-console .card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.log-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  font-size: 13px;
  color: #606266;
}
.log-box {
  background: #1e1e1e;
  color: #d4d4d4;
  border-radius: 4px;
  padding: 10px;
  height: 380px;
  overflow-y: auto;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
}
.log-item {
  margin-bottom: 6px;
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.log-time {
  color: #888;
  flex-shrink: 0;
}
.log-tag {
  flex-shrink: 0;
  width: 50px;
  text-align: center;
  border-radius: 3px;
  font-size: 11px;
  font-weight: 600;
  padding: 1px 4px;
}
.log-send .log-tag { background: #1e88e5; color: #fff; }
.log-recv .log-tag { background: #43a047; color: #fff; }
.log-sys .log-tag  { background: #fb8c00; color: #fff; }
.log-binary .log-tag { background: #8e24aa; color: #fff; }
.log-content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  flex: 1;
}
.file-info {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
</style>