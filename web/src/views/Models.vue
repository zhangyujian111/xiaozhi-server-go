<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { internalApi } from '@/api'

interface ModelRow {
  id: number
  tenantId: number | null
  modelId: string
  provider: string
  baseUrl: string
  apiKey: string
  upstreamModel: string
  modality: string
  type: 'chat' | 'asr' | 'tts' | 'embedding'
  contextLength: number
  priceInputCents: number
  priceOutputCents: number
  isStream: boolean
  isDefault: boolean
  capabilities: string
  status: number
}

const loading = ref(false)
const rows = ref<ModelRow[]>([])
const typeFilter = ref<string>('') // '' / chat / asr / tts / embedding
const statusFilter = ref<string>('') // '' / 0 / 1
const search = ref('')

const DEFAULT_INTERNAL_TOKEN = 'dev-internal-token'
const storedRaw = localStorage.getItem('aisaas_internal_token')
const rawDebug = ref<string>(JSON.stringify(storedRaw))
const currentToken = ref<string>(typeof storedRaw === 'string' && storedRaw && storedRaw !== '[object Object]' ? storedRaw : '')

function fillDefaultToken() {
  localStorage.setItem('aisaas_internal_token', DEFAULT_INTERNAL_TOKEN)
  currentToken.value = DEFAULT_INTERNAL_TOKEN
  ElMessage.success(`已写入 dev-internal-token，请刷新页面`)
}

function diagnoseAndFix() {
  const raw = localStorage.getItem('aisaas_internal_token')
  const info = {
    raw,
    type: typeof raw,
    length: raw?.length ?? 0,
    isObjectString: raw === '[object Object]'
  }
  console.log('[Models] localStorage[aisaas_internal_token] =', info)
  if (info.isObjectString || !raw || typeof raw !== 'string') {
    localStorage.setItem('aisaas_internal_token', DEFAULT_INTERNAL_TOKEN)
    currentToken.value = DEFAULT_INTERNAL_TOKEN
    ElMessage.warning(`原值非法 (${JSON.stringify(info)})，已覆盖写入 dev-internal-token，请刷新`)
  } else {
    ElMessage.info(`当前值: ${raw} (length=${raw.length})，刷新仍 401 请检查拼写/空格`)
  }
}

function autoFixOnLoad() {
  const raw = localStorage.getItem('aisaas_internal_token')
  if (!raw || typeof raw !== 'string' || raw === '[object Object]') {
    console.warn('[Models] autoFix: localStorage 脏值，自动写入 dev-internal-token', { raw, type: typeof raw })
    localStorage.setItem('aisaas_internal_token', DEFAULT_INTERNAL_TOKEN)
    currentToken.value = DEFAULT_INTERNAL_TOKEN
    ElMessage.warning(`检测到 localStorage[aisaas_internal_token] 非法 (${JSON.stringify(raw)})，已自动写入 dev-internal-token，正在刷新页面…`)
    setTimeout(() => window.location.reload(), 800)
    return true
  }
  return false
}

// 在 module 加载时就跑一次（早于 onMounted，早于 axios interceptor）
;(function bootstrapFix() {
  const raw = localStorage.getItem('aisaas_internal_token')
  if (!raw || typeof raw !== 'string' || raw === '[object Object]') {
    console.warn('[Models][bootstrap] localStorage 脏值，自动写入 dev-internal-token', { raw, type: typeof raw })
    localStorage.setItem('aisaas_internal_token', DEFAULT_INTERNAL_TOKEN)
  }
})()

const dialogVisible = ref(false)
const dialogMode = ref<'create' | 'edit'>('create')
const editing = reactive<Partial<ModelRow>>({})

async function load() {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (typeFilter.value) params.type = typeFilter.value
    if (statusFilter.value !== '') params.status = statusFilter.value
    const { data } = await internalApi.get('/v1/models', { params })
    rows.value = (data.data || []) as ModelRow[]
  } catch (e: any) {
    const status = e?.response?.status
    const code = e?.response?.data?.code
    const msg = e?.response?.data?.message || e?.message || ''
    if (status === 401 && code === 40104) {
      ElMessage.error(`40104: token 值不匹配 aisaas 配置。当前 localStorage=${currentToken ? '"' + currentToken + '"' : '空'}，aisaas 期望="dev-internal-token"`)
    } else if (status === 401) {
      ElMessage.error('401 未授权：未携带 X-Internal-Token（请去「连接设置」填写并保存）')
    } else {
      ElMessage.error(`加载失败 [${status}]: ${msg}`)
    }
  } finally {
    loading.value = false
  }
}

const filtered = computed(() => {
  const k = search.value.trim().toLowerCase()
  if (!k) return rows.value
  return rows.value.filter(r =>
    r.modelId.toLowerCase().includes(k) ||
    r.provider.toLowerCase().includes(k) ||
    r.upstreamModel.toLowerCase().includes(k)
  )
})

function openCreate() {
  dialogMode.value = 'create'
  Object.assign(editing, {
    id: undefined,
    modelId: '',
    provider: 'bailian',
    baseUrl: 'https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
    apiKey: '',
    upstreamModel: '',
    modality: '["text"]',
    type: 'chat',
    contextLength: 32768,
    priceInputCents: 0,
    priceOutputCents: 0,
    isStream: true,
    isDefault: false,
    capabilities: '{}',
    status: 1
  } as Partial<ModelRow>)
  dialogVisible.value = true
}

function openEdit(row: ModelRow) {
  dialogMode.value = 'edit'
  Object.assign(editing, { ...row })
  dialogVisible.value = true
}

async function save() {
  if (!editing.modelId || !editing.baseUrl || !editing.apiKey || !editing.type) {
    ElMessage.warning('modelId / baseUrl / apiKey / type 必填')
    return
  }
  try {
    if (dialogMode.value === 'create') {
      await internalApi.post('/v1/models', editing)
      ElMessage.success('已创建')
    } else {
      // 编辑模式下 apiKey 为空表示不改
      const body: any = { ...editing }
      if (!body.apiKey) delete body.apiKey
      delete body.id
      await internalApi.put(`/v1/models/${editing.id}`, body)
      ElMessage.success('已保存')
    }
    dialogVisible.value = false
    load()
  } catch (e: any) {
    ElMessage.error('保存失败：' + (e?.response?.data?.message || e?.message))
  }
}

async function toggleStatus(row: ModelRow) {
  const next = row.status === 1 ? 0 : 1
  try {
    await internalApi.put(`/v1/models/${row.id}`, { status: next })
    ElMessage.success(next ? '已启用' : '已停用')
    load()
  } catch (e: any) {
    ElMessage.error('切换失败：' + (e?.response?.data?.message || e?.message))
  }
}

async function setDefault(row: ModelRow) {
  if (!confirm(`将 "${row.modelId}" 设为默认 ${row.type} 模型？\n当前默认将被取消。`)) return
  try {
    // 取消其他默认
    const others = rows.value.filter(r => r.type === row.type && r.id !== row.id && r.isDefault)
    for (const o of others) {
      await internalApi.put(`/v1/models/${o.id}`, { isDefault: false })
    }
    await internalApi.put(`/v1/models/${row.id}`, { isDefault: true })
    ElMessage.success('已设为默认')
    load()
  } catch (e: any) {
    ElMessage.error('设置失败：' + (e?.response?.data?.message || e?.message))
  }
}

async function remove(row: ModelRow) {
  try {
    await ElMessageBox.confirm(`确定软删除模型 "${row.modelId}"？该操作仅做软删，后续可以从数据库恢复。`, '删除确认', {
      confirmButtonText: '删除',
      cancelButtonText: '取消',
      type: 'warning'
    })
  } catch { return }
  try {
    await internalApi.delete(`/v1/models/${row.id}`)
    ElMessage.success('已删除')
    load()
  } catch (e: any) {
    ElMessage.error('删除失败：' + (e?.response?.data?.message || e?.message))
  }
}

// 测试连接
const testing = ref(false)
const testResult = ref<any>(null)

async function testModel(row: ModelRow) {
  testing.value = true
  testResult.value = null
  try {
    const { data } = await internalApi.post('/v1/models/test', {
      baseUrl: row.baseUrl,
      apiKey: row.apiKey,
      upstreamModel: row.upstreamModel || row.modelId,
      type: row.type
    })
    testResult.value = data
    if (data.data?.status === 200) ElMessage.success('连接成功')
    else ElMessage.warning(`上游返回 ${data.data?.status}`)
  } catch (e: any) {
    testResult.value = { error: e?.message }
    ElMessage.error('测试失败：' + (e?.message || ''))
  } finally {
    testing.value = false
  }
}

// 显示 apiKey 时前 8 后 4
function maskKey(k: string | undefined): string {
  if (!k) return ''
  if (k.length <= 14) return k
  return k.slice(0, 8) + '...' + k.slice(-4)
}

onMounted(() => {
  autoFixOnLoad()
  load()
})
</script>

<template>
  <div class="models-view">
    <div class="diag-box">
      <strong>DEBUG:</strong> localStorage[aisaas_internal_token] =
      <code>{{ rawDebug }}</code>
      <el-button size="small" type="primary" @click="fillDefaultToken">写入默认值</el-button>
      <el-button size="small" @click="diagnoseAndFix">诊断</el-button>
    </div>

    <el-alert
      v-if="!currentToken"
      type="error"
      :closable="false"
      show-icon
      style="margin-bottom: 12px"
    >
      <template #title>
        localStorage[aisaas_internal_token] 非法或为空 — 401 根因
      </template>
      <template #default>
        <el-button type="primary" size="small" @click="fillDefaultToken">
          一键写入 dev-internal-token
        </el-button>
        <el-button size="small" @click="diagnoseAndFix">诊断并自动修复</el-button>
        <span style="margin-left: 12px; color: #94a3b8; font-size: 12px">
          写完后请刷新本页（Ctrl+F5）
        </span>
      </template>
    </el-alert>

    <el-card shadow="never">
      <template #header>
        <div class="header">
          <span class="title">模型注册表</span>
          <span class="subtitle">ykt_aisaas_model_registry · chat / asr / tts / embedding</span>
        </div>
      </template>

      <div class="toolbar">
        <el-select v-model="typeFilter" placeholder="类型" clearable style="width:120px" @change="load">
          <el-option label="全部" value="" />
          <el-option label="chat" value="chat" />
          <el-option label="asr" value="asr" />
          <el-option label="tts" value="tts" />
          <el-option label="embedding" value="embedding" />
        </el-select>
        <el-select v-model="statusFilter" placeholder="状态" clearable style="width:120px" @change="load">
          <el-option label="全部" value="" />
          <el-option label="启用" value="1" />
          <el-option label="停用" value="0" />
        </el-select>
        <el-input v-model="search" placeholder="搜索 modelId / provider / upstreamModel" style="width:300px" clearable />
        <el-button @click="load" :loading="loading">刷新</el-button>
        <el-button type="primary" @click="openCreate">+ 新增模型</el-button>
      </div>

      <el-table :data="filtered" v-loading="loading" border stripe style="width:100%">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="type" label="类型" width="80">
          <template #default="{ row }">
            <el-tag :type="row.type==='chat'?'primary':row.type==='asr'?'success':row.type==='tts'?'warning':'info'" size="small">
              {{ row.type }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="modelId" label="modelId" min-width="180" />
        <el-table-column prop="upstreamModel" label="上游模型" min-width="160" />
        <el-table-column prop="provider" label="provider" width="120" />
        <el-table-column label="baseUrl" min-width="280" show-overflow-tooltip>
          <template #default="{ row }">
            <code class="url">{{ row.baseUrl }}</code>
          </template>
        </el-table-column>
        <el-table-column label="apiKey" width="200">
          <template #default="{ row }">
            <code class="key">{{ maskKey(row.apiKey) }}</code>
          </template>
        </el-table-column>
        <el-table-column prop="contextLength" label="ctx" width="80" />
        <el-table-column label="默认" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.isDefault" type="success" size="small">默认</el-tag>
            <el-button v-else link type="primary" @click="setDefault(row)">设为默认</el-button>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-switch :model-value="row.status===1" @change="toggleStatus(row)" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="240" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="testModel(row)" :loading="testing">测试</el-button>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="testResult" class="test-result">
        <div class="test-title">测试结果</div>
        <pre>{{ JSON.stringify(testResult, null, 2) }}</pre>
      </div>
    </el-card>

    <!-- 新增/编辑对话框 -->
    <el-dialog v-model="dialogVisible" :title="dialogMode==='create'?'新增模型':'编辑模型'" width="640px">
      <el-form label-width="120px">
        <el-form-item label="modelId" required>
          <el-input v-model="editing.modelId" :disabled="dialogMode==='edit'" placeholder="qwen3.6-flash" />
        </el-form-item>
        <el-form-item label="provider" required>
          <el-input v-model="editing.provider" placeholder="bailian / openai / custom" />
        </el-form-item>
        <el-form-item label="type" required>
          <el-select v-model="editing.type" style="width:100%">
            <el-option label="chat" value="chat" />
            <el-option label="asr" value="asr" />
            <el-option label="tts" value="tts" />
            <el-option label="embedding" value="embedding" />
          </el-select>
        </el-form-item>
        <el-form-item label="baseUrl" required>
          <el-input v-model="editing.baseUrl" placeholder="https://..." />
        </el-form-item>
        <el-form-item label="apiKey" :required="dialogMode==='create'">
          <el-input v-model="editing.apiKey" type="password" show-password :placeholder="dialogMode==='edit'?'留空表示不修改':'sk-...'" />
        </el-form-item>
        <el-form-item label="upstreamModel">
          <el-input v-model="editing.upstreamModel" placeholder="上游实际模型名；留空则用 modelId" />
        </el-form-item>
        <el-form-item label="modality">
          <el-input v-model="editing.modality" placeholder='["text"] / ["text","audio"]' />
        </el-form-item>
        <el-form-item label="contextLength">
          <el-input-number v-model="editing.contextLength" :min="0" :step="1024" />
        </el-form-item>
        <el-form-item label="价格 (分)">
          <el-input-number v-model="editing.priceInputCents" :min="0" :step="1" /> 输入
          <el-input-number v-model="editing.priceOutputCents" :min="0" :step="1" /> 输出
        </el-form-item>
        <el-form-item label="流式">
          <el-switch v-model="editing.isStream" />
        </el-form-item>
        <el-form-item label="默认">
          <el-switch v-model="editing.isDefault" />
        </el-form-item>
        <el-form-item label="状态">
          <el-switch v-model="editing.status" :active-value="1" :inactive-value="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.models-view { padding: 16px; }
.header { display: flex; align-items: baseline; gap: 12px; }
.title { font-size: 16px; font-weight: 600; }
.subtitle { font-size: 12px; color: #94a3b8; }
.toolbar { display: flex; gap: 12px; margin-bottom: 12px; align-items: center; }
.url { font-family: monospace; font-size: 12px; color: #475569; }
.key { font-family: monospace; font-size: 12px; color: #64748b; }
.test-result {
  margin-top: 16px;
  padding: 12px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 4px;
}
.test-title { font-weight: 600; margin-bottom: 8px; }
.test-result pre { margin: 0; font-size: 12px; max-height: 300px; overflow: auto; }
</style>