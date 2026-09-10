<template>
  <div class="mcp">
    <el-alert
      title="aisaas 端点：POST /api/v1/mcp/tools/test（需 Bearer API Key）"
      type="info"
      :closable="false"
      show-icon
      style="margin-bottom: 16px"
    />

    <el-row :gutter="20">
      <el-col :span="10">
        <el-card>
          <template #header>
            <span>工具调用 (test)</span>
          </template>
          <el-form label-width="100px">
            <el-form-item label="toolId">
              <el-input v-model="toolId" placeholder="工具 ID（先调用下方'工具列表'获取）" />
            </el-form-item>
            <el-form-item label="arguments">
              <el-input
                v-model="argsText"
                type="textarea"
                :rows="10"
                placeholder='{"city": "北京"}'
              />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="testTool" :loading="loading">调用</el-button>
              <el-button @click="argsText = '{}'">清空参数</el-button>
            </el-form-item>
          </el-form>
        </el-card>
      </el-col>

      <el-col :span="14">
        <el-card>
          <template #header>
            <span>返回</span>
          </template>
          <div v-if="callResult" class="result">
            <pre class="result-json">{{ JSON.stringify(callResult, null, 2) }}</pre>
          </div>
          <el-empty v-else description="尚无调用结果" />
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 16px">
      <template #header>
        <div class="card-header">
          <span>工具列表</span>
          <el-button @click="listTools" :loading="listing">刷新</el-button>
        </div>
      </template>
      <el-table :data="tools" stripe size="small" :empty-text="'点击刷新加载'">
        <el-table-column prop="id" label="id" width="100" />
        <el-table-column prop="name" label="name" width="200" />
        <el-table-column prop="description" label="description" />
      </el-table>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { aisaasApi } from '@/api'

const toolId = ref('')
const argsText = ref('{\n  "city": "北京"\n}')
const loading = ref(false)
const listing = ref(false)
const callResult = ref<any>(null)
const tools = ref<any[]>([])

async function testTool() {
  if (!toolId.value) {
    ElMessage.warning('请输入 toolId')
    return
  }
  let args: any
  try {
    args = argsText.value.trim() ? JSON.parse(argsText.value) : {}
  } catch (e: any) {
    ElMessage.error(`JSON 解析失败: ${e.message}`)
    return
  }
  loading.value = true
  try {
    const r = await aisaasApi.post('/api/v1/mcp/tools/test', {
      toolId: toolId.value,
      args
    })
    callResult.value = r.data
  } catch (e: any) {
    callResult.value = { success: false, error: e?.response?.data || e?.message }
  } finally {
    loading.value = false
  }
}

async function listTools() {
  listing.value = true
  try {
    const r = await aisaasApi.get('/api/v1/mcp/tools')
    tools.value = r.data?.items || r.data?.list || r.data || []
    ElMessage.success(`加载到 ${tools.value.length} 个工具`)
  } catch (e: any) {
    ElMessage.error(`加载失败: ${e?.response?.status || ''} ${e?.message}`)
  } finally {
    listing.value = false
  }
}
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.result {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.result-json {
  background: #f5f7fa;
  border-radius: 4px;
  padding: 12px;
  font-size: 12px;
  max-height: 500px;
  overflow: auto;
}
</style>