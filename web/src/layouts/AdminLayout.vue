<template>
  <el-container class="layout-root">
    <el-aside width="220px" class="aside">
      <div class="logo">
        <el-icon class="logo-icon"><Connection /></el-icon>
        <span>Device Dev Console</span>
      </div>
      <el-menu
        :default-active="$route.path"
        router
        background-color="#001529"
        text-color="#a6adb4"
        active-text-color="#fff"
      >
        <el-menu-item index="/dashboard">
          <el-icon><Monitor /></el-icon>
          <span>系统总览</span>
        </el-menu-item>
        <el-menu-item index="/devices">
          <el-icon><Iphone /></el-icon>
          <span>设备激活</span>
        </el-menu-item>
        <el-menu-item index="/ota">
          <el-icon><Upload /></el-icon>
          <span>OTA 升级</span>
        </el-menu-item>
        <el-menu-item index="/ws">
          <el-icon><ChatDotRound /></el-icon>
          <span>设备 WebSocket</span>
        </el-menu-item>
        <el-menu-item index="/chat">
          <el-icon><Promotion /></el-icon>
          <span>文本对话</span>
        </el-menu-item>
        <el-menu-item index="/settings">
          <el-icon><Setting /></el-icon>
          <span>连接设置</span>
        </el-menu-item>
        <el-menu-item index="/portal" @click="goPortal">
          <el-icon><Link /></el-icon>
          <span>AI 配置中心 → /portal</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="header">
        <div class="header-left">
          <span class="page-title">{{ $route.meta.title }}</span>
        </div>
        <div class="header-right">
          <el-tag v-if="auth.internalToken" type="success" size="small">Internal Token 已配置</el-tag>
          <el-tag v-else type="warning" size="small">未配置 Internal Token</el-tag>
          <el-dropdown v-if="auth.adminUsername" trigger="click" @command="handleCommand">
            <span class="user-info">
              <el-icon><UserFilled /></el-icon>
              {{ auth.adminUsername }}
              <el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="logout">退出登录</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <el-main class="main">
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import {
  Monitor,
  Upload,
  ChatDotRound,
  Promotion,
  Setting,
  Connection,
  Link,
  Iphone,
  UserFilled,
  ArrowDown
} from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const auth = useAuthStore()
const router = useRouter()

function goPortal() {
  const base = (window.location.origin || `${location.protocol}//${location.host}`) + '/portal/'
  window.location.href = base
}

function handleCommand(cmd: string) {
  if (cmd === 'logout') {
    auth.clearAdminSession()
    ElMessage.info('已退出登录')
    router.push('/login')
  }
}
</script>

<style scoped>
.layout-root {
  height: 100vh;
}
.aside {
  background: #001529;
  color: #fff;
}
.logo {
  height: 60px;
  display: flex;
  align-items: center;
  padding-left: 20px;
  font-size: 16px;
  font-weight: 600;
  color: #fff;
  border-bottom: 1px solid #1f3a5c;
}
.logo-icon {
  margin-right: 8px;
  font-size: 20px;
  color: #409eff;
}
.el-menu {
  border-right: none;
}
.header {
  background: #fff;
  border-bottom: 1px solid #e6e6e6;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.page-title {
  font-size: 18px;
  font-weight: 600;
  color: #303133;
}
.main {
  background: #f5f7fa;
  padding: 20px;
  overflow: auto;
}
.header-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.user-info {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  color: #606266;
  font-size: 14px;
  padding: 4px 8px;
  border-radius: 4px;
  transition: background 0.2s;
}
.user-info:hover {
  background: #f5f7fa;
}
</style>