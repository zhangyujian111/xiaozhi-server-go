import { createRouter, createWebHistory } from 'vue-router'
import AdminLayout from '@/layouts/AdminLayout.vue'

// 路由说明：
// - /xiaozhi-admin/ 是 ESP32 设备管理专用页（OTA / WebSocket 调试 / 设备连接设置）
// - 模型/记忆/人设/会话/MCP 等 AI 平台配置已迁到 /portal (ykt-aisaas web) 统一管理
const router = createRouter({
  history: createWebHistory('/xiaozhi-admin/'),
  routes: [
    {
      path: '/',
      component: AdminLayout,
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '系统总览' } },
        { path: 'ota', name: 'ota', component: () => import('@/views/OTA.vue'), meta: { title: 'OTA 升级' } },
        { path: 'ws', name: 'ws', component: () => import('@/views/WebSocket.vue'), meta: { title: '设备 WebSocket' } },
        { path: 'chat', name: 'chat', component: () => import('@/views/Chat.vue'), meta: { title: '文本对话' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '连接设置' } }
      ]
    }
  ]
})

export default router