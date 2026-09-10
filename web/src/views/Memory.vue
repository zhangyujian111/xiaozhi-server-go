<template>
  <div class="memory">
    <el-alert
      title="aisaas 端点：GET/POST /api/v1/memories/{deviceId}/messages（需 X-Internal-Token）"
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom: 16px"
    />

    <el-card>
      <el-form :inline="true">
        <el-form-item label="deviceId">
          <el-input v-model="deviceId" style="width: 200px" />
        </el-form-item>
        <el-form-item label="limit">
          <el-input-number v-model="limit" :min="1" :max="200" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="loadMessages">查询</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card style="margin-top: 16px">
      <template #header>
        <span>消息列表 ({{ messages.length }})</span>
      </template>
      <el-table :data="messages" stripe size="small" :empty-text="'暂无数据'">
        <el-table-column prop="role" label="role" width="100" />
        <el-table-column prop="sessionId" label="sessionId" width="200" />
        <el-table-column prop="content" label="content" />
        <el-table-column prop="createdAt" label="createdAt" width="180" />
      </el-table>
    </el-card>

    <el-card style="margin-top: 16px">
      <template #header>
        <span>写入测试消息（POST）</span>
      </template>
      <el-form label-width="100px">
        <el-form-item label="role">
          <el-radio-group v-model="writeRole">
            <el-radio-button label="user">user</el-radio-button>
            <el-radio-button label="assistant">assistant</el-radio-button>
            <el-radio-button label="system">system</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="content">
          <el-input v-model="writeContent" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="sessionId">
          <el-input v-model="writeSessionId" />
        </el-form-item>
        <el-form-item>
          <el-button type="success" @click="postMessage">写入</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { aisaasApi } from '@/api'

const deviceId = ref('ESP32-001')
const limit = ref(50)
const messages = ref<any[]>([])

const writeRole = ref('user')
const writeContent = ref('测试记忆内容')
const writeSessionId = ref('')

async function loadMessages() {
  if (!deviceId.value) {
    ElMessage.warning('请输入 deviceId')
    return
  }
  try {
    const r = await aisaasApi.get(`/api/v1/memories/${encodeURIComponent(deviceId.value)}/messages`, {
      params: { limit: limit.value }
    })
    messages.value = r.data?.items || r.data?.list || r.data || []
    ElMessage.success(`加载到 ${messages.value.length} 条`)
  } catch (e: any) {
    ElMessage.error(`查询失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}

async function postMessage() {
  try {
    await aisaasApi.post(`/api/v1/memories/${encodeURIComponent(deviceId.value)}/messages`, {
      role: writeRole.value,
      content: writeContent.value,
      sessionId: writeSessionId.value || undefined
    })
    ElMessage.success('写入成功')
    await loadMessages()
  } catch (e: any) {
    ElMessage.error(`写入失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}
</script>