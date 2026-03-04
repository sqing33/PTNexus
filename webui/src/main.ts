// src/main.ts

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import axios from 'axios'
import { getDesktopBridge } from './desktop/bridge'

// 引入 Element Plus
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

// 引入公共毛玻璃样式
import './assets/styles/glass-morphism.scss'

// 引入全局字体设置
import './assets/styles/typography.scss'

// 引入移动端响应式覆盖
import './assets/styles/mobile-responsive.scss'
import './assets/styles/status-toast.scss'

// 如果需要，可以引入 ECharts
import * as echarts from 'echarts'

const uint8ArrayToBase64 = (bytes: Uint8Array): string => {
  let binary = ''
  const chunkSize = 0x8000
  for (let i = 0; i < bytes.length; i += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunkSize))
  }
  return btoa(binary)
}

const base64ToUint8Array = (base64: string): Uint8Array => {
  if (!base64) return new Uint8Array()
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes
}

const isArrayBufferView = (value: unknown): value is ArrayBufferView => ArrayBuffer.isView(value)

const shouldProxyDesktopURL = (url: string): boolean =>
  url === '/health' || url.startsWith('/api') || url.startsWith('/update')

// fetch 全局包装：为所有 /api 请求自动附加 Bearer Token，并在 401 时跳转登录
const originalFetch = window.fetch.bind(window)
window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
  const url = typeof input === 'string' || input instanceof URL ? String(input) : input.url
  const token = localStorage.getItem('token')

  // 合并并设置 Authorization 头
  const mergedHeaders = new Headers(
    (typeof input !== 'string' && !(input instanceof URL)
      ? (input as Request).headers
      : undefined) ||
      init?.headers ||
      {},
  )
  if (token && url.startsWith('/api') && !mergedHeaders.has('Authorization')) {
    mergedHeaders.set('Authorization', `Bearer ${token}`)
  }

  const finalInit: RequestInit = { ...init, headers: mergedHeaders }
  const finalInput =
    typeof input === 'string' || input instanceof URL ? url : new Request(input, finalInit)

  const desktopBridge = getDesktopBridge()
  if (desktopBridge && shouldProxyDesktopURL(url)) {
    const request =
      typeof finalInput === 'string' ? new Request(finalInput, finalInit) : (finalInput as Request)

    const headers: Record<string, string> = {}
    request.headers.forEach((value, key) => {
      headers[key] = value
    })

    const method = String(request.method || 'GET').toUpperCase()
    let bodyBase64 = ''
    if (method !== 'GET' && method !== 'HEAD') {
      const buf = await request.arrayBuffer()
      if (buf.byteLength > 0) {
        bodyBase64 = uint8ArrayToBase64(new Uint8Array(buf))
      }
    }

    const desktopResp = await desktopBridge.DesktopRequest({
      method,
      url,
      headers,
      bodyBase64,
    })

    const respBytes = base64ToUint8Array(desktopResp.bodyBase64 || '')
    const respHeaders = new Headers(desktopResp.headers || {})
    const resp = new Response(respBytes, { status: desktopResp.status, headers: respHeaders })

    if (resp.status === 401 && !url.startsWith('/api/auth/')) {
      const current = router.currentRoute.value
      const redirect = encodeURIComponent(current.fullPath)
      router.replace(`/login?redirect=${redirect}`)
    }
    return resp
  }

  const resp = await originalFetch(finalInput, typeof input === 'string' || input instanceof URL ? finalInit : undefined)
  if (resp.status === 401 && !url.startsWith('/api/auth/')) {
    const current = router.currentRoute.value
    const redirect = encodeURIComponent(current.fullPath)
    router.replace(`/login?redirect=${redirect}`)
  }
  return resp
}

// Axios 全局拦截：为所有请求附加 Bearer Token，并处理 401
axios.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers = config.headers || {}
    config.headers['Authorization'] = `Bearer ${token}`
  }

  // 修复cookie泄露问题：确保不发送不必要的cookie
  config.withCredentials = false

  // 设置更严格的请求头，避免携带其他应用的cookie
  if (config.headers) {
    config.headers['X-Requested-With'] = 'XMLHttpRequest'
  }

  return config
})

axios.interceptors.response.use(
  (resp) => resp,
  (error) => {
    const url = String(error?.config?.url || '')
    if (error?.response?.status === 401 && !url.startsWith('/api/auth/')) {
      const current = router.currentRoute.value
      const redirect = encodeURIComponent(current.fullPath)
      router.replace(`/login?redirect=${redirect}`)
    }
    return Promise.reject(error)
  },
)

// Desktop Runtime: axios 适配器改为走 Wails 原生绑定（避免前端直接访问 HTTP API）
const originalAdapter = axios.defaults.adapter
axios.defaults.adapter = async (config) => {
  const method = String(config.method || 'get').toUpperCase()
  const baseURL = String((config as { baseURL?: unknown }).baseURL || '')
  const url = String(config.url || '')

  let resolved = url
  if (baseURL && !url.startsWith('http') && !url.startsWith('/')) {
    resolved = `${baseURL.replace(/\/+$/, '')}/${url.replace(/^\/+/, '')}`
  }

  let finalURL = resolved
  if (config.params && typeof finalURL === 'string' && finalURL.startsWith('/')) {
    const u = new URL(finalURL, window.location.origin)
    for (const [key, value] of Object.entries(config.params as Record<string, unknown>)) {
      if (value === undefined || value === null) continue
      if (Array.isArray(value)) {
        value.forEach((v) => u.searchParams.append(key, String(v)))
      } else {
        u.searchParams.append(key, String(value))
      }
    }
    finalURL = u.pathname + u.search + u.hash
  }

  const desktopBridgeForAxios = getDesktopBridge()
  if (!desktopBridgeForAxios || !shouldProxyDesktopURL(finalURL)) {
    if (typeof originalAdapter === 'function') {
      return originalAdapter(config)
    }
    throw new Error(`no axios adapter for url: ${finalURL}`)
  }

  const headersObj = (() => {
    const raw = config.headers as unknown
    if (!raw || typeof raw !== 'object') return {}
    const maybeToJSON = (raw as { toJSON?: unknown }).toJSON
    if (typeof maybeToJSON === 'function') {
      return (raw as { toJSON: () => Record<string, unknown> }).toJSON()
    }
    return raw as Record<string, unknown>
  })()

  const headers: Record<string, string> = {}
  for (const [key, value] of Object.entries(headersObj)) {
    if (value === undefined || value === null) continue
    if (Array.isArray(value)) {
      headers[key] = value.map((v) => String(v)).join(', ')
    } else {
      headers[key] = String(value)
    }
  }

  const bodyText = ''
  let bodyBase64 = ''

  if (method !== 'GET' && method !== 'HEAD') {
    let bodyInit: BodyInit | undefined = undefined
    const data = (config as { data?: unknown }).data
    if (data !== undefined && data !== null) {
      if (typeof data === 'string') {
        bodyInit = data
      } else if (data instanceof URLSearchParams) {
        bodyInit = data
      } else if (data instanceof FormData) {
        bodyInit = data
      } else if (data instanceof Blob) {
        bodyInit = data
      } else if (data instanceof ArrayBuffer) {
        bodyInit = data
      } else if (isArrayBufferView(data)) {
        bodyInit = data
      } else if (typeof data === 'object') {
        bodyInit = JSON.stringify(data)
        if (!headers['Content-Type'] && !headers['content-type']) {
          headers['Content-Type'] = 'application/json'
        }
      } else {
        bodyInit = String(data)
      }
    }

    if (bodyInit !== undefined) {
      const request = new Request(finalURL, { method, headers, body: bodyInit })
      const normalizedHeaders: Record<string, string> = {}
      request.headers.forEach((value, key) => {
        normalizedHeaders[key] = value
      })

      const buf = await request.arrayBuffer()
      if (buf.byteLength > 0) {
        bodyBase64 = uint8ArrayToBase64(new Uint8Array(buf))
      }
      Object.assign(headers, normalizedHeaders)
    }
  }

  if (!bodyBase64 && bodyText) {
    // noop: 保留字段，便于以后扩展
  }

  const desktopResp = await desktopBridgeForAxios.DesktopRequest({
    method,
    url: finalURL,
    headers,
    bodyText,
    bodyBase64,
    timeoutMs: typeof config.timeout === 'number' ? config.timeout : undefined,
  })

  const respBytes = base64ToUint8Array(desktopResp.bodyBase64 || '')
  const contentType =
    desktopResp.headers?.['Content-Type'] ||
    desktopResp.headers?.['content-type'] ||
    'application/octet-stream'

  let data: unknown = undefined
  const responseType = String((config as { responseType?: unknown }).responseType || '')
  if (responseType === 'arraybuffer') {
    data = respBytes.buffer.slice(respBytes.byteOffset, respBytes.byteOffset + respBytes.byteLength)
  } else if (responseType === 'blob') {
    data = new Blob([respBytes], { type: contentType })
  } else {
    const text = new TextDecoder('utf-8').decode(respBytes)
    if (responseType === 'text') {
      data = text
    } else if (contentType.includes('application/json')) {
      try {
        data = text ? JSON.parse(text) : null
      } catch {
        data = text
      }
    } else {
      data = text
    }
  }

  return {
    data,
    status: desktopResp.status,
    statusText: desktopResp.statusText || '',
    headers: desktopResp.headers || {},
    config,
    request: null,
  }
}

const app = createApp(App)

app.use(createPinia())

// 将 echarts 挂载到全局，方便组件中使用
app.config.globalProperties.$echarts = echarts

app.use(router)

app.use(ElementPlus, {
  locale: zhCn,
})

app.mount('#app')
