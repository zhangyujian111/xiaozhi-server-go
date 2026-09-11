<template>
  <div class="login-page">
    <el-card class="login-card" shadow="hover">
      <template #header>
        <div class="login-header">
          <el-icon class="logo-icon"><Connection /></el-icon>
          <span>小智设备管理后台</span>
        </div>
      </template>

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        @keyup.enter="handleLogin"
      >
        <el-form-item label="账号" prop="username">
          <el-input
            v-model="form.username"
            placeholder="admin"
            clearable
            autofocus
            data-test="login-username"
          />
        </el-form-item>

        <el-form-item label="密码" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="admin123"
            show-password
            data-test="login-password"
          />
        </el-form-item>

        <el-alert
          v-if="errorMsg"
          :title="errorMsg"
          type="error"
          show-icon
          :closable="false"
          class="error-alert"
        />

        <el-button
          type="primary"
          :loading="loading"
          style="width: 100%"
          @click="handleLogin"
          data-test="login-submit"
        >
          登录
        </el-button>
      </el-form>

      <div class="login-footer">
        <el-text size="small" type="info">
          默认账号 <code>admin</code> / 密码 <code>admin123</code>
        </el-text>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { Connection } from '@element-plus/icons-vue'
import { adminLogin } from '@/api'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const formRef = ref<FormInstance>()
const form = reactive({
  username: '',
  password: ''
})

const rules: FormRules = {
  username: [{ required: true, message: '请输入账号', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
}

const loading = ref(false)
const errorMsg = ref('')

async function handleLogin() {
  if (!formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  errorMsg.value = ''
  try {
    const resp = await adminLogin(form.username, form.password)
    // 解析 expiresAt (RFC3339) → unix seconds
    const exp = Math.floor(new Date(resp.expiresAt).getTime() / 1000)
    auth.saveAdminSession(resp.token, resp.username, exp)
    ElMessage.success(`欢迎回来，${resp.username}`)
    const target = (route.query.redirect as string) || '/devices'
    router.push(target)
  } catch (err: any) {
    const status = err?.response?.status
    if (status === 401) {
      errorMsg.value = '账号或密码错误'
    } else if (status === 400) {
      errorMsg.value = '请求格式错误：' + (err?.response?.data?.error ?? err.message)
    } else {
      errorMsg.value = `登录失败: ${err?.response?.data?.error ?? err.message}`
    }
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #001529 0%, #1f3a5c 100%);
}

.login-card {
  width: 380px;
  border-radius: 8px;
}

.login-header {
  display: flex;
  align-items: center;
  font-size: 18px;
  font-weight: 600;
}

.logo-icon {
  margin-right: 8px;
  font-size: 22px;
  color: #409eff;
}

.error-alert {
  margin-bottom: 16px;
}

.login-footer {
  margin-top: 16px;
  text-align: center;
  color: #909399;
}

code {
  background: #f0f9ff;
  padding: 1px 6px;
  border-radius: 3px;
  font-family: 'Consolas', monospace;
  color: #303133;
}
</style>