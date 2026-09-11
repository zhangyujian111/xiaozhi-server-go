import { createRouter, createWebHistory } from 'vue-router'
import AdminLayout from '@/layouts/AdminLayout.vue'
import { useAuthStore } from '@/stores/auth'

// 路由说明：
//   - /xiaozhi-admin/ 是 ESP32 设备管理专用页（OTA / WebSocket 调试 / 设备连接设置）
//   - 模型/记忆/人设/会话/MCP 等 AI 平台配置已迁到 /portal (ykt-aisaas web) 统一管理
//
// 鉴权策略：
//   - /login 页面无需鉴权
//   - 其他所有页面要求 admin JWT (xz_admin_token)
//   - 过期/无效 → 自动跳 /login?redirect=xxx
const router = createRouter({
  history: createWebHistory('/xiaozhi-admin/'),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/Login.vue'),
      meta: { title: '管理员登录', public: true }
    },
    {
      path: '/',
      component: AdminLayout,
      children: [
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'dashboard', component: () => import('@/views/Dashboard.vue'), meta: { title: '系统总览' } },
        { path: 'devices', name: 'devices', component: () => import('@/views/Devices.vue'), meta: { title: '设备激活' } },
        { path: 'ota', name: 'ota', component: () => import('@/views/OTA.vue'), meta: { title: 'OTA 升级' } },
        { path: 'ws', name: 'ws', component: () => import('@/views/WebSocket.vue'), meta: { title: '设备 WebSocket' } },
        { path: 'chat', name: 'chat', component: () => import('@/views/Chat.vue'), meta: { title: '文本对话' } },
        { path: 'settings', name: 'settings', component: () => import('@/views/Settings.vue'), meta: { title: '连接设置' } }
      ]
    }
  ]
})

// 全局前置守卫：未登录跳 /login
router.beforeEach((to, _from, next) => {
  const auth = useAuthStore()
  auth.load() // 刷新 token（防止 token 在新标签页写入但 store 未同步）

  if (to.meta.public) {
    // 已登录访问 /login → 直接跳到 dashboard
    if (to.name === 'login' && auth.isAdminAuthenticated()) {
      return next('/dashboard')
    }
    return next()
  }
  if (!auth.isAdminAuthenticated()) {
    return next({ path: '/login', query: { redirect: to.fullPath } })
  }
  next()
})

export default router