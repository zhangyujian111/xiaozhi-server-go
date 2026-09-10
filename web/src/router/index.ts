import { createRouter, createWebHistory } from 'vue-router'
import AdminLayout from '@/layouts/AdminLayout.vue'

const router = createRouter({
  history: createWebHistory(),
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
        { path: 'memory', name: 'memory', component: () => import('@/views/Memory.vue'), meta: { title: '记忆管理' } },
        { path: 'sessions', name: 'sessions', component: () => import('@/views/Sessions.vue'), meta: { title: '会话管理' } },
        { path: 'personas', name: 'personas', component: () => import('@/views/Personas.vue'), meta: { title: '人设' } },
        { path: 'mcp', name: 'mcp', component: () => import('@/views/MCP.vue'), meta: { title: 'MCP 工具' } },
        { path: 'models', name: 'models', component: () => import('@/views/Models.vue'), meta: { title: '模型管理' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '连接设置' } }
      ]
    }
  ]
})

export default router