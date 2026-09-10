<template>
  <div class="chat">
    <el-alert
      title="旁路 aisaas /v1/chat/completions（需 Bearer API Key）。xiaozhi-server-go 协议只有音频通道，没有文本 method；本页直接调 aisaas LLM"
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom: 16px"
    />

    <el-row :gutter="20">
      <el-col :span="18">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>对话</span>
              <div>
                <el-button size="small" @click="loadModels">加载模型列表</el-button>
                <el-button size="small" type="danger" @click="clearConversation">清空对话</el-button>
              </div>
            </div>
          </template>

          <div ref="messagesRef" class="messages">
            <div v-for="(m, i) in messages" :key="i" :class="['msg', `msg-${m.role}`]">
              <div class="msg-meta">
                <el-tag size="small" :type="m.role === 'user' ? 'primary' : 'success'">{{ m.role }}</el-tag>
                <span class="msg-time">{{ m.time }}</span>
                <span v-if="m.tokens" class="msg-tokens">{{ m.tokens }} tokens</span>
              </div>
              <div class="msg-content">{{ m.content }}<span v-if="m.streaming" class="cursor">▌</span></div>
            </div>
          </div>

          <el-divider />

          <el-form @submit.prevent="send">
            <el-form-item>
              <el-input
                v-model="input"
                type="textarea"
                :rows="3"
                placeholder="输入消息，按 Ctrl+Enter 发送"
                @keydown.ctrl.enter="send"
              />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="send" :loading="loading" :disabled="!input.trim()">发送 (Ctrl+Enter)</el-button>
              <el-button v-if="loading" @click="abort">停止</el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <el-col :span="6">
        <el-card>
          <template #header><span>参数</span></template>
          <el-form label-width="80px" size="small">
            <el-form-item label="model">
              <el-select v-model="model" style="width: 100%">
                <el-option v-for="m in modelList" :key="m.id" :label="m.id" :value="m.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="temperature">
              <el-input-number v-model="temperature" :min="0" :max="2" :step="0.1" style="width: 100%" />
            </el-form-item>
            <el-form-item label="max_tokens">
              <el-input-number v-model="maxTokens" :min="50" :max="8000" :step="100" style="width: 100%" />
            </el-form-item>
            <el-form-item label="MCP">
              <el-switch v-model="useMcp" />
              <div class="hint">启用租户 MCP 工具</div>
            </el-form-item>
            <el-form-item label="流式">
              <el-switch v-model="stream" />
              <div class="hint">SSE 流式响应</div>
            </el-form-item>
          </el-form>
        </el-card>

        <el-card style="margin-top: 16px">
          <template #header><span>系统提示词（可选）</span></template>
          <el-input v-model="systemPrompt" type="textarea" :rows="6" placeholder="system message" />
        </el-card>

        <el-card style="margin-top: 16px">
          <template #header><span>TTS（说完转语音）</span></template>
          <el-form label-width="60px" size="small">
            <el-form-item label="voice">
              <el-input v-model="ttsVoice" placeholder="alloy/echo/..." />
            </el-form-item>
            <el-form-item label="speed">
              <el-input-number v-model="ttsSpeed" :min="0.5" :max="2" :step="0.1" style="width: 100%" />
            </el-form-item>
            <el-form-item>
              <el-button size="small" @click="speakLast" :disabled="!lastAssistantText">朗读最后回复</el-button>
            </el-form-item>
            <audio v-if="audioUrl" :src="audioUrl" controls style="width: 100%; margin-top: 8px" />
          </el-form>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import axios from 'axios'

interface Message {
  role: 'user' | 'assistant' | 'system'
  content: string
  time: string
  tokens?: number
}

const auth = useAuthStore()

const messages = ref<(Message & { streaming?: boolean })[]>([])
const input = ref('')
const model = ref('qwen3.6-flash')
const modelList = ref<any[]>([])
const temperature = ref(0.7)
const maxTokens = ref(2000)
const useMcp = ref(false)
const stream = ref(true)
const systemPrompt = ref('你是一个测试用 AI 助手')
const loading = ref(false)
const messagesRef = ref<HTMLElement | null>(null)
let abortCtrl: AbortController | null = null

const ttsVoice = ref('alloy')
const ttsSpeed = ref(1)
const audioUrl = ref<string | null>(null)

function nowStr() {
  return new Date().toLocaleTimeString('zh-CN')
}

function scrollBottom() {
  nextTick(() => {
    if (messagesRef.value) messagesRef.value.scrollTop = messagesRef.value.scrollHeight
  })
}

async function loadModels() {
  try {
    const headers: any = {}
    if (auth.bearerKey) headers.Authorization = `Bearer ${auth.bearerKey}`
    const r = await axios.get('/v1/models', { headers })
    modelList.value = r.data?.data || r.data || []
    if (modelList.value.length && !modelList.value.find((m: any) => m.id === model.value)) {
      model.value = modelList.value[0].id
    }
    ElMessage.success(`加载到 ${modelList.value.length} 个模型`)
  } catch (e: any) {
    ElMessage.error(`加载模型失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}

function clearConversation() {
  messages.value = []
}

function buildMessages(): any[] {
  const out: any[] = []
  if (systemPrompt.value.trim()) {
    out.push({ role: 'system', content: systemPrompt.value })
  }
  for (const m of messages.value) {
    out.push({ role: m.role, content: m.content })
  }
  return out
}

async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return

  messages.value.push({ role: 'user', content: text, time: nowStr() })
  input.value = ''
  scrollBottom()

  const assistantIdx = messages.value.length
  messages.value.push({ role: 'assistant', content: '', time: nowStr(), streaming: true })
  scrollBottom()

  loading.value = true
  abortCtrl = new AbortController()

  const body: any = {
    model: model.value,
    messages: buildMessages(),
    stream: stream.value,
    temperature: temperature.value,
    max_tokens: maxTokens.value,
    'x-tools-mcp': useMcp.value
  }

  const headers: any = { 'Content-Type': 'application/json' }
  if (auth.bearerKey) headers.Authorization = `Bearer ${auth.bearerKey}`

  try {
    if (stream.value) {
      const r = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal: abortCtrl.signal
      })
      if (!r.ok || !r.body) {
        const errText = await r.text()
        throw new Error(`HTTP ${r.status}: ${errText.slice(0, 200)}`)
      }
      const reader = r.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() || ''
        let curEvent = ''
        for (const line of lines) {
          if (line.startsWith('event:')) {
            curEvent = line.slice(6).trim()
            continue
          }
          if (!line.startsWith('data:')) continue
          const payload = line.slice(5).trim()
          if (payload === '[DONE]') continue
          try {
            const obj = JSON.parse(payload)
            // aisaas 业务错误事件：x-error / error
            if (curEvent === 'x-error' || obj.error || obj.code === 50201) {
              const msg = obj.message || obj.error?.message || '未知错误'
              messages.value[assistantIdx].content = `[错误] ${msg}`
              messages.value[assistantIdx].streaming = false
              ElMessage.error(`LLM 调用失败: ${msg.slice(0, 100)}`)
              continue
            }
            const delta = obj.choices?.[0]?.delta?.content || ''
            if (delta) {
              messages.value[assistantIdx].content += delta
              scrollBottom()
            }
          } catch {}
        }
      }
    } else {
      const r = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal: abortCtrl.signal
      })
const data = await r.json()
        if (data.error) {
          messages.value[assistantIdx].content = `[错误] ${data.error.message || JSON.stringify(data.error)}`
          ElMessage.error(`LLM 调用失败: ${data.error.message?.slice(0, 100) || '未知'}`)
        } else {
          messages.value[assistantIdx].content = data.choices?.[0]?.message?.content || JSON.stringify(data)
          messages.value[assistantIdx].tokens = data.usage?.total_tokens
        }
    }
  } catch (e: any) {
    if (e.name !== 'AbortError') {
      messages.value[assistantIdx].content = `[ERROR] ${e.message}`
    }
  } finally {
    messages.value[assistantIdx].streaming = false
    loading.value = false
    abortCtrl = null
    scrollBottom()
  }
}

function abort() {
  if (abortCtrl) abortCtrl.abort()
}

const lastAssistantText = ref('')
import { watch } from 'vue'
watch(
  () => messages.value.filter((m) => m.role === 'assistant' && !m.streaming).slice(-1)[0]?.content,
  (v) => { lastAssistantText.value = v || '' }
)

async function speakLast() {
  if (!lastAssistantText.value) {
    ElMessage.warning('没有可朗读的回复')
    return
  }
  const headers: any = { 'Content-Type': 'application/json' }
  if (auth.bearerKey) headers.Authorization = `Bearer ${auth.bearerKey}`
  try {
    const r = await axios.post('/v1/audio/speech', {
      model: 'tts-1',
      input: lastAssistantText.value,
      voice: ttsVoice.value,
      speed: ttsSpeed.value
    }, { headers, responseType: 'blob' })
    if (audioUrl.value) URL.revokeObjectURL(audioUrl.value)
    audioUrl.value = URL.createObjectURL(r.data as Blob)
    ElMessage.success('已生成语音')
  } catch (e: any) {
    ElMessage.error(`TTS 失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}

onMounted(() => {
  loadModels()
})
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.messages {
  height: 480px;
  overflow-y: auto;
  padding: 10px;
  background: #fafafa;
  border-radius: 4px;
  border: 1px solid #ebeef5;
}
.msg {
  margin-bottom: 12px;
  padding: 10px 14px;
  border-radius: 8px;
  max-width: 85%;
}
.msg-user {
  background: #409eff;
  color: #fff;
  margin-left: auto;
}
.msg-assistant {
  background: #fff;
  border: 1px solid #ebeef5;
}
.msg-meta {
  font-size: 12px;
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.msg-user .msg-meta { color: rgba 255,255,255,0.85; }
.msg-time { opacity: 0.7; font-size: 11px; }
.msg-tokens { opacity: 0.7; font-size: 11px; }
.msg-content {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.6;
}
.cursor {
  display: inline-block;
  animation: blink 1s infinite;
  margin-left: 2px;
}
@keyframes blink { 50% { opacity: 0; } }
.hint {
  font-size: 11px;
  color: #909399;
  margin-top: 2px;
}
</style>