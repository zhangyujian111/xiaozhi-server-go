import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAuthStore = defineStore('auth', () => {
  const internalToken = ref('')
  const bearerKey = ref('')
  const xzTarget = ref('')

  // Admin web (xiaozhi-admin) JWT
  const adminToken = ref('')
  const adminUsername = ref('')
  const adminTokenExpiresAt = ref(0) // Unix seconds

  function load() {
    internalToken.value = localStorage.getItem('aisaas_internal_token') || ''
    bearerKey.value = localStorage.getItem('aisaas_bearer_key') || ''
    xzTarget.value = localStorage.getItem('xz_target') || ''

    adminToken.value = localStorage.getItem('xz_admin_token') || ''
    adminUsername.value = localStorage.getItem('xz_admin_username') || ''
    const exp = parseInt(localStorage.getItem('xz_admin_exp') || '0', 10)
    adminTokenExpiresAt.value = isNaN(exp) ? 0 : exp
  }

  function saveInternalToken(token: string) {
    internalToken.value = token
    localStorage.setItem('aisaas_internal_token', token)
  }

  function saveBearerKey(key: string) {
    bearerKey.value = key
    localStorage.setItem('aisaas_bearer_key', key)
  }

  function saveTarget(target: string) {
    xzTarget.value = target
    localStorage.setItem('xz_target', target)
  }

  function saveAdminSession(token: string, username: string, expiresAt: number) {
    adminToken.value = token
    adminUsername.value = username
    adminTokenExpiresAt.value = expiresAt
    localStorage.setItem('xz_admin_token', token)
    localStorage.setItem('xz_admin_username', username)
    localStorage.setItem('xz_admin_exp', String(expiresAt))
  }

  function clearAdminSession() {
    adminToken.value = ''
    adminUsername.value = ''
    adminTokenExpiresAt.value = 0
    localStorage.removeItem('xz_admin_token')
    localStorage.removeItem('xz_admin_username')
    localStorage.removeItem('xz_admin_exp')
  }

  // isAdminAuthenticated: 有 token 且未过期
  function isAdminAuthenticated(): boolean {
    if (!adminToken.value) return false
    if (adminTokenExpiresAt.value && adminTokenExpiresAt.value < Math.floor(Date.now() / 1000)) {
      return false
    }
    return true
  }

  function clear() {
    internalToken.value = ''
    bearerKey.value = ''
    xzTarget.value = ''
    clearAdminSession()
    localStorage.removeItem('aisaas_internal_token')
    localStorage.removeItem('aisaas_bearer_key')
    localStorage.removeItem('xz_target')
  }

  return {
    internalToken,
    bearerKey,
    xzTarget,
    adminToken,
    adminUsername,
    adminTokenExpiresAt,
    load,
    saveInternalToken,
    saveBearerKey,
    saveTarget,
    saveAdminSession,
    clearAdminSession,
    isAdminAuthenticated,
    clear
  }
})