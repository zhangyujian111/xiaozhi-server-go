<template>
  <div class="personas">
    <el-alert
      title="aisaas 端点：GET /api/v1/personas?deviceId=xxx（需 Bearer API Key）"
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
        <el-form-item>
          <el-button type="primary" @click="load">查询人设</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-row :gutter="20" style="margin-top: 16px">
      <el-col :span="12" v-for="p in personas" :key="p.personaId || p.id">
        <el-card class="persona-card">
          <template #header>
            <div class="card-header">
              <span>
                <el-icon><User /></el-icon>
                {{ p.name || p.personaId }}
              </span>
              <el-tag size="small" :type="p.isDefault ? 'success' : 'info'">
                {{ p.isDefault ? '默认' : '可选' }}
              </el-tag>
            </div>
          </template>
          <div class="persona-id">ID: {{ p.personaId || p.id }}</div>
          <div class="persona-desc">{{ p.description || p.systemPrompt?.slice(0, 200) || '—' }}</div>
          <pre class="persona-json">{{ JSON.stringify(p, null, 2) }}</pre>
        </el-card>
      </el-col>
    </el-row>

    <el-empty v-if="personas.length === 0 && loaded" description="暂无人设" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { User } from '@element-plus/icons-vue'
import { aisaasApi } from '@/api'

const deviceId = ref('ESP32-001')
const personas = ref<any[]>([])
const loaded = ref(false)

async function load() {
  if (!deviceId.value) {
    ElMessage.warning('请输入 deviceId')
    return
  }
  try {
    const r = await aisaasApi.get('/api/v1/personas', {
      params: { deviceId: deviceId.value }
    })
    personas.value = r.data?.items || r.data?.list || r.data || []
    loaded.value = true
    ElMessage.success(`查询到 ${personas.value.length} 条人设`)
  } catch (e: any) {
    ElMessage.error(`查询失败: ${e?.response?.status || ''} ${e?.message}`)
  }
}
</script>

<style scoped>
.persona-card {
  margin-bottom: 16px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.persona-id {
  font-size: 12px;
  color: #909399;
  margin-bottom: 8px;
}
.persona-desc {
  font-size: 13px;
  color: #606266;
  margin-bottom: 8px;
}
.persona-json {
  background: #f5f7fa;
  border-radius: 4px;
  padding: 8px;
  font-size: 11px;
  max-height: 200px;
  overflow: auto;
}
</style>