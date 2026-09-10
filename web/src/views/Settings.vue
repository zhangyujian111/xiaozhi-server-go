<template>
  <div class="settings">
    <el-card>
      <template #header>
        <span>后端连接配置</span>
      </template>
      <el-form label-width="160px">
        <el-form-item label="xiaozhi-server-go 地址">
          <el-input v-model="xzTarget" placeholder="http://localhost:8080" />
          <div class="hint">仅用于显示，Vite 代理已硬编码为 :8080，修改需重启 Vite</div>
        </el-form-item>
        <el-form-item label="ykt-aisaas 地址">
          <el-input v-model="aisaasTarget" placeholder="http://localhost:8190 (硬编码)" disabled />
          <div class="hint">同上，硬编码于 vite.config.ts</div>
        </el-form-item>
        <el-form-item label="Internal Token">
          <el-input
            v-model="internalToken"
            type="password"
            show-password
            placeholder="dev-internal-token"
          />
          <div class="hint">aisaas 内部接口 <code>X-Internal-Token</code> 头；用于 <code>/internal/api/v1/*</code> 与 <code>/api/v1/ping</code></div>
        </el-form-item>
        <el-form-item label="Bearer API Key">
          <el-input
            v-model="bearerKey"
            type="password"
            show-password
            placeholder="sk-aisaas-xxxxxxxxxxxx"
          />
          <div class="hint">aisaas 公共接口 <code>Authorization: Bearer sk-aisaas-...</code> 头；用于 <code>/api/v1/personas/*</code>、<code>/api/v1/memories/*</code>、<code>/api/v1/sessions/*</code>、<code>/api/v1/mcp/tools/*</code></div>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="save">保存</el-button>
          <el-button @click="clear">清空</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card style="margin-top: 16px">
      <template #header>
        <span>连通性测试</span>
      </template>
      <el-form label-width="160px">
        <el-form-item label="xiaozhi-server-go">
          <el-button @click="testXz" :loading="testingXz">GET /healthz</el-button>
          <span v-if="xzResult" class="test-result" :class="{ ok: xzOk }">{{ xzResult }}</span>
        </el-form-item>
        <el-form-item label="ykt-aisaas (no auth)">
          <el-button @click="testAisaasNoAuth" :loading="testingAisaasNoAuth">GET /healthz</el-button>
          <span v-if="aisaasNoAuthResult" class="test-result" :class="{ ok: aisaasNoAuthOk }">{{ aisaasNoAuthResult }}</span>
        </el-form-item>
        <el-form-item label="ykt-aisaas (Bearer)">
          <el-button @click="testAisaasBearer" :loading="testingAisaasBearer" :disabled="!bearerKey">GET /api/v1/personas</el-button>
          <span v-if="aisaasBearerResult" class="test-result" :class="{ ok: aisaasBearerOk }">{{ aisaasBearerResult }}</span>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { xzApi, aisaasApi } from '@/api'

const auth = useAuthStore()
const xzTarget = ref('')
const aisaasTarget = ref('http://localhost:8190 (硬编码)')
const internalToken = ref('')
const bearerKey = ref('')

const testingXz = ref(false)
const testingAisaasNoAuth = ref(false)
const testingAisaasBearer = ref(false)
const xzResult = ref('')
const aisaasNoAuthResult = ref('')
const aisaasBearerResult = ref('')
const xzOk = ref(false)
const aisaasNoAuthOk = ref(false)
const aisaasBearerOk = ref(false)

onMounted(() => {
  xzTarget.value = auth.xzTarget || 'http://localhost:8080'
  internalToken.value = auth.internalToken
  bearerKey.value = auth.bearerKey
})

function save() {
  auth.saveInternalToken(internalToken.value)
  auth.saveBearerKey(bearerKey.value)
  auth.saveTarget(xzTarget.value)
  ElMessage.success('已保存（刷新页面生效）')
}

function clear() {
  auth.clear()
  internalToken.value = ''
  bearerKey.value = ''
  xzTarget.value = ''
  ElMessage.success('已清空')
}

async function testXz() {
  testingXz.value = true
  try {
    const r = await xzApi.get('/healthz')
    xzResult.value = `✓ ${JSON.stringify(r.data)}`
    xzOk.value = true
  } catch (e: any) {
    xzResult.value = `✗ ${e?.message}`
    xzOk.value = false
  } finally {
    testingXz.value = false
  }
}

async function testAisaasNoAuth() {
  testingAisaasNoAuth.value = true
  try {
    const r = await aisaasApi.get('/healthz')
    aisaasNoAuthResult.value = `✓ ${JSON.stringify(r.data)}`
    aisaasNoAuthOk.value = true
  } catch (e: any) {
    aisaasNoAuthResult.value = `✗ ${e?.message || ''} (status=${e?.response?.status})`
    aisaasNoAuthOk.value = false
  } finally {
    testingAisaasNoAuth.value = false
  }
}

async function testAisaasBearer() {
  testingAisaasBearer.value = true
  try {
    const r = await aisaasApi.get('/api/v1/personas', { params: { deviceId: 'ESP32-001' } })
    aisaasBearerResult.value = `✓ ${JSON.stringify(r.data).slice(0, 200)}`
    aisaasBearerOk.value = true
  } catch (e: any) {
    aisaasBearerResult.value = `✗ ${e?.response?.status || ''} ${e?.message}`
    aisaasBearerOk.value = false
  } finally {
    testingAisaasBearer.value = false
  }
}
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
code {
  background: #f0f2f5;
  padding: 1px 4px;
  border-radius: 3px;
  font-family: monospace;
  font-size: 11px;
}
.test-result {
  margin-left: 12px;
  font-size: 12px;
  color: #f56c6c;
}
.test-result.ok {
  color: #67c23a;
}
</style>