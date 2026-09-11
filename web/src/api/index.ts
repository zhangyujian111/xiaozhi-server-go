import axios from 'axios'

export const xzApi = axios.create({
  baseURL: '/api/xz',
  timeout: 15000
})

export const aisaasApi = axios.create({
  baseURL: '/api/aisaas',
  timeout: 30000
})

export const internalApi = axios.create({
  baseURL: '/internal/api',
  timeout: 30000
})

// xiaozhi-admin 后台管理 API（JWT 鉴权）
export const adminApi = axios.create({
  baseURL: '/api/admin',
  timeout: 15000
})

function refreshAuthHeaders(config: any) {
  const internalToken = localStorage.getItem('aisaas_internal_token') || ''
  const bearerKey = localStorage.getItem('aisaas_bearer_key') || ''
  if (internalToken) config.headers['X-Internal-Token'] = internalToken
  if (bearerKey)   config.headers['Authorization'] = `Bearer ${bearerKey}`
  if (config.url?.startsWith('/v1/models') || config.url?.includes('/internal/api')) {
    console.log('[axios]', config.method?.toUpperCase(), config.url, '| X-Internal-Token=', JSON.stringify(config.headers['X-Internal-Token']))
  }
  return config
}

function refreshAdminAuthHeaders(config: any) {
  const adminToken = localStorage.getItem('xz_admin_token') || ''
  if (adminToken) {
    config.headers['Authorization'] = `Bearer ${adminToken}`
  }
  return config
}

xzApi.interceptors.request.use(refreshAuthHeaders)
aisaasApi.interceptors.request.use(refreshAuthHeaders)
internalApi.interceptors.request.use(refreshAuthHeaders)
adminApi.interceptors.request.use(refreshAdminAuthHeaders)

xzApi.interceptors.response.use(
  (r) => r,
  (err) => {
    console.error('[xzApi]', err.config?.url, err.response?.status, err.message)
    return Promise.reject(err)
  }
)

aisaasApi.interceptors.response.use(
  (r) => r,
  (err) => {
    console.error('[aisaasApi]', err.config?.url, err.response?.status, err.message)
    return Promise.reject(err)
  }
)
internalApi.interceptors.response.use(
  (r) => r,
  (err) => {
    console.error('[internalApi]', err.config?.url, err.response?.status, err.response?.data, err.message)
    return Promise.reject(err)
  }
)

adminApi.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response?.status === 401) {
      // token 过期或无效 → 清空本地 session（router 守卫会跳 login）
      localStorage.removeItem('xz_admin_token')
      localStorage.removeItem('xz_admin_username')
      localStorage.removeItem('xz_admin_exp')
      console.warn('[adminApi] 401 — admin token expired or invalid')
    }
    console.error('[adminApi]', err.config?.url, err.response?.status, err.response?.data, err.message)
    return Promise.reject(err)
  }
)

// ============================================================================
// Admin API 封装
// ============================================================================

export interface AdminLoginResponse {
  token: string;
  expiresAt: string; // RFC3339
  username: string;
}

export interface AdminDevice {
  deviceId: string;
  chipModel: string;
  version: string;
  deviceType: string;
  ipAddress: string;
  wifiSsid: string;
  activated: boolean;
  activatedAt: number; // unix seconds; 0 表示待激活
  code: string;         // 关联的 6 位激活码
}

export function adminLogin(username: string, password: string) {
  return adminApi.post<AdminLoginResponse>('/auth/login', { username, password }).then((r) => r.data)
}

export function adminMe() {
  return adminApi.get<{ username: string; role: string }>('/auth/me').then((r) => r.data)
}

export function adminActivateByCode(code: string) {
  return adminApi
    .post<{ deviceId: string; activatedAt: number }>('/devices/activate-by-code', { code })
    .then((r) => r.data)
}

export function adminListPending() {
  return adminApi.get<{ items: AdminDevice[]; count: number }>('/devices/pending').then((r) => r.data)
}

export function adminListActivated() {
  return adminApi.get<{ items: AdminDevice[]; count: number }>('/devices/activated').then((r) => r.data)
}