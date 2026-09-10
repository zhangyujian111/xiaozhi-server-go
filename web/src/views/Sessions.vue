<template>
  <div class="sessions">
    <el-alert
      title="aisaas 端点：POST /api/v1/sessions/{deviceId}（创建）、GET /api/v1/sessions/{deviceId}/history（历史）、POST /api/v1/sessions/{deviceId}/{sessionId}/end（结束）"
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom: 16px"
    />

    <el-row :gutter="20">
      <el-col :span="12">
        <el-card>
          <template #header>
            <span>创建会话 (POST)</span>
          </template>
          <el-form label-width="100px">
            <el-form-item label="deviceId">
              <el-input v-model="createDeviceId" />
            </el-form-item>
            <el-form-item label="persona">
              <el-input v-model="createPersonaId" placeholder="可选" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="createSession">创建</el-button>
            </el-form-item>
          </el-form>

          <el-divider />

          <div v-if="createResult">
            <div class="result-label">创建结果：</div>
            <pre class="result-json">{{ JSON.stringify(createResult, null, 2) }}</pre>
          </div>
        </el-card>
      </el-col>

      <el-col :span="12">
        <el-card>
          <template #header>
            <span>查询会话历史 (GET history)</span>
          </template>
          <el-form :inline="true">
            <el-form-item label="deviceId">
              <el-input v-model="listDeviceId" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="listSessions">查询</el-button>
            </el-form-item>
          </el-form>

          <el-table :data="sessionList" stripe size="small" :empty-text="'暂无会话'">
            <el-table-column prop="sessionId" label="sessionId" />
            <el-table-column prop="status" label="status" width="100" />
            <el-table-column prop="createdAt" label="createdAt" width="180" />
            <el-table-column label="操作" width="100">
              <template #default="{ row }">
                <el-button size="small" type="danger" link @click="endSession(row.sessionId)">结束</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { aisaasApi } from '@/api'

const createDeviceId = ref('ESP32-001')
const createPersonaId = ref('')
const createResult = ref<any>(null)

const listDeviceId = ref('ESP32-001')
const sessionList = ref<any[]>([])

async function createSession() {
  try {
    const r = await aisaasApi.post(`/api/v1/sessions/${encodeURIComponent(createDeviceId.value)}`, {
      personaId: createPersonaId.value || undefined
    })
    createResult.value = r.data
    ElMessage.success('创建成功')
  } catch (e: any) {
    createResult.value = { error: e?.response?.data || e?.message }
    ElMessage.error('创建失败')
  }
}

async function listSessions() {
  try {
    const r = await aisaasApi.get(`/api/v1/sessions/${encodeURIComponent(listDeviceId.value)}/history`)
    sessionList.value = r.data?.items || r.data?.list || r.data || []
    ElMessage.success(`查询到 ${sessionList.value.length} 条`)
  } catch (e: any) {
    ElMessage.error(`查询失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}

async function endSession(sessionId: string) {
  try {
    await ElMessageBox.confirm(`确认结束会话 ${sessionId} ?`, '警告', { type: 'warning' })
  } catch {
    return
  }
  try {
    await aisaasApi.post(`/api/v1/sessions/${encodeURIComponent(listDeviceId.value)}/${encodeURIComponent(sessionId)}/end`, {
      reason: 'admin_end'
    })
    ElMessage.success('已结束')
    await listSessions()
  } catch (e: any) {
    ElMessage.error(`结束失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}
</script>

<style scoped>
.result-label {
  font-size: 13px;
  color: #909399;
  margin-bottom: 6px;
}
.result-json {
  background: #f5f7fa;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  padding: 10px;
  font-size: 12px;
  max-height: 240px;
  overflow: auto;
}
</style>