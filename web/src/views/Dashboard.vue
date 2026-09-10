<template>
  <div class="dev-console">
    <el-row :gutter="20">
      <el-col :span="8">
        <el-card class="metric-card">
          <div class="metric-label">xiaozhi-server-go</div>
          <div class="metric-value">
            <el-tag v-if="health" type="success" size="large">运行中</el-tag>
            <el-tag v-else-if="healthError" type="danger" size="large">不可达</el-tag>
            <el-tag v-else type="info" size="large">检测中…</el-tag>
          </div>
          <div class="metric-meta" v-if="health">{{ health.time }}</div>
          <div class="metric-meta error" v-else-if="healthError">{{ healthError }}</div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="metric-card">
          <div class="metric-label">ykt-aisaas</div>
          <div class="metric-value">
            <el-tag v-if="aisaasHealth" type="success" size="large">在线</el-tag>
            <el-tag v-else type="danger" size="large">不可达</el-tag>
          </div>
          <div class="metric-meta">{{ aisaasVersion }}</div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="metric-card">
          <div class="metric-label">活跃 WebSocket 连接</div>
          <div class="metric-value">{{ wsCount ?? '—' }}</div>
          <div class="metric-meta">ws_connections_active (Prometheus)</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px">
      <el-col :span="12">
        <el-card>
          <template #header><span>快速连接 ESP32 设备</span></template>
          <ol class="quick-start">
            <li>设备烧录 <code>xiaozhi-esp32</code> 固件（v1.8+）</li>
            <li>设备启动后扫码或手动填入配网参数</li>
            <li>配网参数：
              <pre>OTA_URL: <strong>http://&lt;xz-host&gt;:8080/api/device/ota</strong>
WS_URL: ws://&lt;xz-host&gt;:8080/api/ws
TOKEN:  &lt;从设备列表生成&gt;</pre>
            </li>
            <li>去 <strong>设备 WebSocket</strong> 页面观察连接状态 / 收发消息</li>
            <li>去 <strong>文本对话</strong> 页面模拟用户输入，验证对话链路</li>
          </ol>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card>
          <template #header><span>SDK 状态检查</span></template>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="xiaozhi-server-go">
              <el-tag v-if="health" type="success" size="small">健康</el-tag>
              <el-tag v-else type="danger" size="small">异常</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="readyz">
              <el-tag v-if="ready && (ready.status === 'ok' || ready.status === 'ready')" type="success" size="small">就绪</el-tag>
              <el-tag v-else type="warning" size="small">未就绪</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="ykt-aisaas">
              <el-tag v-if="aisaasHealth" type="success" size="small">健康</el-tag>
              <el-tag v-else type="danger" size="small">异常</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="活跃连接">
              {{ wsCount ?? '—' }}
            </el-descriptions-item>
            <el-descriptions-item label="Bearer Key">
              <el-tag v-if="auth.bearerKey" type="success" size="small">已配置</el-tag>
              <el-tag v-else type="warning" size="small">未配置</el-tag>
              <el-link type="primary" :underline="false" style="margin-left:8px" @click="$router.push('/settings')">
                配置
              </el-link>
            </el-descriptions-item>
            <el-descriptions-item label="Internal Token">
              <el-tag v-if="auth.internalToken" type="success" size="small">已配置</el-tag>
              <el-tag v-else type="warning" size="small">未配置</el-tag>
              <el-link type="primary" :underline="false" style="margin-left:8px" @click="$router.push('/settings')">
                配置
              </el-link>
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" style="margin-top: 20px">
      <el-col :span="24">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>Prometheus 原始输出</span>
              <el-button size="small" @click="loadAll">刷新</el-button>
            </div>
          </template>
          <el-input
            type="textarea"
            :rows="10"
            v-model="metricsText"
            readonly
            resize="none"
            style="font-family: monospace; font-size: 12px;"
          />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { xzApi } from '@/api'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const health = ref<{ status: string; time: string } | null>(null)
const healthError = ref<string>('')
const ready = ref<{ status: string } | null>(null)
const aisaasHealth = ref<boolean>(false)
const aisaasVersion = ref<string>('')
const metricsText = ref('')
const wsCount = ref<number | null>(null)
let timer: number | undefined

async function checkHealth() {
  try { const r = await xzApi.get('/healthz'); health.value = r.data; healthError.value = '' }
  catch (e: any) { health.value = null; healthError.value = e?.message || 'request failed' }
}
async function checkReady() {
  try { const r = await xzApi.get('/readyz'); ready.value = r.data }
  catch { ready.value = { status: 'error' } }
}
async function checkAisaas() {
  try {
    const r = await axios.get('/healthz', { baseURL: '/api/aisaas', timeout: 5000 }).catch(() => null)
    if (r && r.status === 200) {
      aisaasHealth.value = true
      aisaasVersion.value = typeof r.data === 'object' ? JSON.stringify(r.data) : String(r.data).slice(0, 50)
    } else { aisaasHealth.value = false; aisaasVersion.value = '' }
  } catch { aisaasHealth.value = false; aisaasVersion.value = '' }
}
async function loadMetrics() {
  try {
    const r = await axios.get('/metrics', { baseURL: '/api/xz' })
    metricsText.value = r.data
    const m = metricsText.value.match(/^ws_connections_active\s+(\d+)/m)
    if (m) wsCount.value = Number(m[1])
  } catch (e: any) { metricsText.value = `(metrics 不可用: ${e?.message})` }
}
async function loadAll() {
  await Promise.all([checkHealth(), checkReady(), checkAisaas(), loadMetrics()])
}
onMounted(() => { loadAll(); timer = window.setInterval(loadAll, 10000) })
onUnmounted(() => { if (timer) clearInterval(timer) })
</script>

<style scoped>
.metric-card { height: 130px; }
.metric-label { font-size: 13px; color: #909399; margin-bottom: 10px; }
.metric-value { font-size: 22px; font-weight: 600; color: #303133; }
.metric-meta { margin-top: 8px; font-size: 12px; color: #909399; }
.metric-meta.error { color: #f56c6c; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.quick-start { padding-left: 20px; margin: 8px 0; line-height: 1.8; }
.quick-start pre { margin: 6px 0; padding: 8px; background: #f5f7fa; border-radius: 4px; font-size: 12px; }
</style>