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

xzApi.interceptors.request.use(refreshAuthHeaders)
aisaasApi.interceptors.request.use(refreshAuthHeaders)
internalApi.interceptors.request.use(refreshAuthHeaders)

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