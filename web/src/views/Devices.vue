<template>
  <div class="devices-page">
    <el-row :gutter="20">
      <!-- 左侧：激活码录入 -->
      <el-col :xs="24" :md="12">
        <el-card shadow="hover" class="card-activate">
          <template #header>
            <div class="card-header">
              <span>设备激活</span>
              <el-tag size="small" type="info">6 位激活码</el-tag>
            </div>
          </template>

          <el-alert
            type="info"
            :closable="false"
            title="操作流程"
            description="1. 设备上电后屏幕显示 6 位激活码（5 分钟有效）&#10;2. 在下方输入框录入该激活码&#10;3. 点击「激活」将设备绑定到本后台&#10;4. 设备下次启动时自动进入工作模式"
            class="info-alert"
          />

          <el-form @submit.prevent="handleActivate">
            <el-form-item label="激活码">
              <el-input
                v-model="codeInput"
                placeholder="123456"
                maxlength="6"
                size="large"
                clearable
                data-test="activate-code-input"
                @input="onCodeInput"
              >
                <template #prepend>
                  <el-icon><Key /></el-icon>
                </template>
              </el-input>
            </el-form-item>

            <el-button
              type="primary"
              size="large"
              :loading="activating"
              :disabled="codeInput.length !== 6"
              @click="handleActivate"
              data-test="activate-submit"
              style="width: 100%"
            >
              激活设备
            </el-button>
          </el-form>

          <div class="hint">
            <el-text size="small" type="info">
              激活码由设备首次启动时生成，在 <code>/api/device/ota/activate</code> 接口返回。
              服务端 Redis 存储 TTL = 5 分钟。
            </el-text>
          </div>
        </el-card>
      </el-col>

      <!-- 右侧：待激活/已激活列表 -->
      <el-col :xs="24" :md="12">
        <el-card shadow="hover">
          <template #header>
            <div class="card-header">
              <el-tabs v-model="activeTab" @tab-change="loadList">
                <el-tab-pane label="待激活" name="pending">
                  <el-badge v-if="pendingList.length > 0" :value="pendingList.length" class="badge" />
                </el-tab-pane>
                <el-tab-pane label="已激活" name="activated">
                  <el-badge v-if="activatedList.length > 0" :value="activatedList.length" class="badge" type="success" />
                </el-tab-pane>
              </el-tabs>
              <el-button size="small" :icon="Refresh" @click="loadList" circle />
            </div>
          </template>

          <!-- 待激活列表 -->
          <div v-if="activeTab === 'pending'">
            <el-empty v-if="!pendingLoading && pendingList.length === 0" description="暂无待激活设备" />
            <el-table v-else: :data="pendingList" stripe size="small" max-height="500">
              <el-table-column prop="deviceId" label="设备 ID" show-overflow-tooltip />
              <el-table-column prop="chipModel" label="芯片" width="100" />
              <el-table-column prop="version" label="版本" width="80" />
              <el-table-column prop="wifiSsid" label="WiFi" show-overflow-tooltip />
              <el-table-column prop="code" label="激活码" width="100">
                <template #default="{ row }">
                  <el-tag size="small" type="warning">{{ row.code }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="ipAddress" label="IP" width="120" />
            </el-table>
          </div>

          <!-- 已激活列表 -->
          <div v-if="activeTab === 'activated'">
            <el-empty v-if="!activatedLoading && activatedList.length === 0" description="暂无已激活设备" />
            <el-table v-else: :data="activatedList" stripe size="small" max-height="500">
              <el-table-column prop="deviceId" label="设备 ID" show-overflow-tooltip />
              <el-table-column prop="chipModel" label="芯片" width="100" />
              <el-table-column prop="version" label="版本" width="80" />
              <el-table-column prop="wifiSsid" label="WiFi" show-overflow-tooltip />
              <el-table-column label="激活时间" width="180">
                <template #default="{ row }">
                  {{ formatTime(row.activatedAt) }}
                </template>
              </el-table-column>
              <el-table-column label="状态" width="80">
                <template #default>
                  <el-tag size="small" type="success">已绑定</el-tag>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Key, Refresh } from '@element-plus/icons-vue'
import { adminActivateByCode, adminListActivated, adminListPending, type AdminDevice } from '@/api'

const codeInput = ref('')
const activating = ref(false)

const activeTab = ref<'pending' | 'activated'>('pending')
const pendingList = ref<AdminDevice[]>([])
const activatedList = ref<AdminDevice[]>([])
const pendingLoading = ref(false)
const activatedLoading = ref(false)

function onCodeInput(val: string) {
  // 强制只允许数字
  codeInput.value = val.replace(/\D/g, '').slice(0, 6)
}

async function handleActivate() {
  if (codeInput.value.length !== 6) {
    ElMessage.warning('请输入完整的 6 位激活码')
    return
  }
  activating.value = true
  try {
    const resp = await adminActivateByCode(codeInput.value)
    ElMessage.success(`设备 ${resp.deviceId} 已激活`)
    codeInput.value = ''
    await loadList()
    // 切到已激活 Tab 让用户立即看到
    activeTab.value = 'activated'
  } catch (err: any) {
    const status = err?.response?.status
    let msg = err?.response?.data?.error ?? err.message
    if (status === 400 && msg?.includes('not found')) {
      msg = '激活码无效或已过期（请让设备重新上电生成新码）'
    }
    ElMessage.error(`激活失败：${msg}`)
  } finally {
    activating.value = false
  }
}

async function loadList() {
  if (activeTab.value === 'pending') {
    pendingLoading.value = true
    try {
      const resp = await adminListPending()
      pendingList.value = resp.items
    } catch (err: any) {
      ElMessage.error('加载待激活列表失败：' + (err?.response?.data?.error ?? err.message))
    } finally {
      pendingLoading.value = false
    }
  } else {
    activatedLoading.value = true
    try {
      const resp = await adminListActivated()
      activatedList.value = resp.items
    } catch (err: any) {
      ElMessage.error('加载已激活列表失败：' + (err?.response?.data?.error ?? err.message))
    } finally {
      activatedLoading.value = false
    }
  }
}

function formatTime(unixSec: number) {
  if (!unixSec) return '-'
  const d = new Date(unixSec * 1000)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  loadList()
  // 每 10 秒自动刷新列表（让"待激活"列表实时显示新设备）
  timer = setInterval(() => {
    if (activeTab.value === 'pending' && !pendingLoading.value) {
      pendingLoading.value = true
      adminListPending()
        .then((r) => (pendingList.value = r.items))
        .catch(() => {})
        .finally(() => (pendingLoading.value = false))
    }
  }, 10_000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.devices-page {
  padding: 0;
}
.card-activate {
  margin-bottom: 16px;
}
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.info-alert {
  margin-bottom: 20px;
}
.hint {
  margin-top: 16px;
  line-height: 1.6;
}
code {
  background: #f0f9ff;
  padding: 1px 6px;
  border-radius: 3px;
  font-family: 'Consolas', monospace;
  color: #303133;
}
.badge {
  margin-left: 8px;
}
:deep(.el-tabs__header) {
  margin-bottom: 0;
}
</style>