import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAuthStore = defineStore('auth', () => {
  const internalToken = ref('')
  const bearerKey = ref('')
  const xzTarget = ref('')

  function load() {
    internalToken.value = localStorage.getItem('aisaas_internal_token') || ''
    bearerKey.value = localStorage.getItem('aisaas_bearer_key') || ''
    xzTarget.value = localStorage.getItem('xz_target') || ''
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

  function clear() {
    internalToken.value = ''
    bearerKey.value = ''
    xzTarget.value = ''
    localStorage.removeItem('aisaas_internal_token')
    localStorage.removeItem('aisaas_bearer_key')
    localStorage.removeItem('xz_target')
  }

  return { internalToken, bearerKey, xzTarget, load, saveInternalToken, saveBearerKey, saveTarget, clear }
})