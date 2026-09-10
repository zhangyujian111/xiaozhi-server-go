<template>
  <div class="ota">
    <el-row :gutter="20">
      <el-col :span="12">
        <el-card>
          <template #header>
            <span>1. OTA 版本检查（GET /api/device/ota）</span>
          </template>
          <el-form :model="checkForm" label-width="120px" size="default">
            <el-form-item label="deviceId">
              <el-input v-model="checkForm.deviceId" placeholder="ESP32-001" />
            </el-form-item>
            <el-form-item label="chipModel">
              <el-select v-model="checkForm.chipModel" style="width: 100%">
                <el-option label="ESP32" value="esp32" />
                <el-option label="ESP32-S3" value="esp32-s3" />
                <el-option label="ESP32-C3" value="esp32-c3" />
              </el-select>
            </el-form-item>
            <el-form-item label="version">
              <el-input v-model="checkForm.version" placeholder="1.0.0" />
            </el-form-item>
            <el-form-item label="deviceType">
              <el-input v-model="checkForm.deviceType" placeholder="speaker" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="doCheck" :loading="checking">检查更新</el-button>
              <el-button @click="reset">重置</el-button>
            </el-form-item>
          </el-form>

          <el-divider />

          <div v-if="checkResult" class="result-block">
            <div class="result-title">返回结果：</div>
            <pre class="result-json">{{ JSON.stringify(checkResult, null, 2) }}</pre>
          </div>
        </el-card>
      </el-col>

      <el-col :span="12">
        <el-card>
          <template #header>
            <span>2. OTA 激活（POST /api/device/ota/activate）</span>
          </template>
          <el-form :model="activateForm" label-width="120px">
            <el-form-item label="deviceId">
              <el-input v-model="activateForm.deviceId" />
            </el-form-item>
            <el-form-item label="chipModel">
              <el-select v-model="activateForm.chipModel" style="width: 100%">
                <el-option label="ESP32" value="esp32" />
                <el-option label="ESP32-S3" value="esp32-s3" />
                <el-option label="ESP32-C3" value="esp32-c3" />
              </el-select>
            </el-form-item>
            <el-form-item label="version">
              <el-input v-model="activateForm.version" />
            </el-form-item>
            <el-form-item label="deviceType">
              <el-input v-model="activateForm.deviceType" />
            </el-form-item>
            <el-form-item>
              <el-button type="success" @click="doActivate" :loading="activating">激活</el-button>
            </el-form-item>
          </el-form>

          <el-divider />

          <div v-if="activateResult" class="result-block">
            <div class="result-title">激活结果：</div>
            <pre class="result-json">{{ JSON.stringify(activateResult, null, 2) }}</pre>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 20px">
      <template #header>
        <span>3. 固件下载（GET /firmware/:firmwareId）</span>
      </template>
      <el-form :inline="true">
        <el-form-item label="firmwareId">
          <el-input v-model="firmwareId" placeholder="如 fw-001 或 fw-001.bin" style="width: 360px" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="downloadFirmware">下载固件</el-button>
        </el-form-item>
      </el-form>
      <el-alert v-if="firmwareMsg" :title="firmwareMsg" :type="firmwareOk ? 'success' : 'error'" :closable="false" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { xzApi } from '@/api'

const checkForm = ref({
  deviceId: 'ESP32-001',
  chipModel: 'esp32-s3',
  version: '1.0.0',
  deviceType: 'speaker'
})

const activateForm = ref({
  deviceId: 'ESP32-001',
  chipModel: 'esp32-s3',
  version: '1.0.0',
  deviceType: 'speaker'
})

const checking = ref(false)
const activating = ref(false)
const checkResult = ref<any>(null)
const activateResult = ref<any>(null)
const firmwareId = ref('')
const firmwareMsg = ref('')
const firmwareOk = ref(false)

async function doCheck() {
  checking.value = true
  try {
    const r = await xzApi.get('/api/device/ota', { params: checkForm.value })
    checkResult.value = r.data
    ElMessage.success('检查完成')
  } catch (e: any) {
    checkResult.value = { error: e?.response?.data || e?.message }
    ElMessage.error('检查失败')
  } finally {
    checking.value = false
  }
}

async function doActivate() {
  activating.value = true
  try {
    const r = await xzApi.post('/api/device/ota/activate', activateForm.value)
    activateResult.value = r.data
    ElMessage.success('激活完成')
  } catch (e: any) {
    activateResult.value = { error: e?.response?.data || e?.message }
    ElMessage.error('激活失败')
  } finally {
    activating.value = false
  }
}

function reset() {
  checkResult.value = null
}

async function downloadFirmware() {
  if (!firmwareId.value) {
    ElMessage.warning('请输入 firmwareId')
    return
  }
  firmwareMsg.value = ''
  firmwareOk.value = false
  try {
    const r = await xzApi.get(`/firmware/${encodeURIComponent(firmwareId.value)}`, {
      responseType: 'blob'
    })
    const size = (r.data as Blob).size
    firmwareMsg.value = `下载成功 (${size} bytes), Content-Type=${r.headers['content-type']}`
    firmwareOk.value = true
    const url = URL.createObjectURL(r.data)
    const a = document.createElement('a')
    a.href = url
    a.download = firmwareId.value
    a.click()
    URL.revokeObjectURL(url)
  } catch (e: any) {
    firmwareMsg.value = `下载失败: ${e?.response?.status || ''} ${e?.message}`
  }
}
</script>

<style scoped>
.result-block {
  margin-top: 8px;
}
.result-title {
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