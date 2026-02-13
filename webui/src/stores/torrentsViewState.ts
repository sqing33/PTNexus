import { defineStore } from 'pinia'
import { ref } from 'vue'
import axios from 'axios'

interface Downloader {
  id: string
  name: string
  enabled?: boolean
}

interface SiteStatus {
  name: string
  site: string
  has_cookie: boolean
  has_passkey: boolean
  is_source: boolean
  is_target: boolean
}

interface UiSettings {
  page_size?: number
  sort_prop?: string
  sort_order?: string | null
  name_search?: string
  active_filters?: {
    paths: string[]
    states: string[]
    existSiteNames: string[]
    notExistSiteNames: string[]
    downloaderIds: string[]
  }
}

const cloneUiSettings = (settings: UiSettings): UiSettings => ({
  page_size: settings.page_size,
  sort_prop: settings.sort_prop,
  sort_order: settings.sort_order,
  name_search: settings.name_search,
  active_filters: {
    paths: [...(settings.active_filters?.paths || [])],
    states: [...(settings.active_filters?.states || [])],
    existSiteNames: [...(settings.active_filters?.existSiteNames || [])],
    notExistSiteNames: [...(settings.active_filters?.notExistSiteNames || [])],
    downloaderIds: [...(settings.active_filters?.downloaderIds || [])],
  },
})

// 用于保存 TorrentsView 页面的初始化状态和缓存数据
// 这个状态在浏览器刷新前会保持，页面切换时不会重置
export const useTorrentsViewState = defineStore('torrentsViewState', () => {
  // 初始化标记
  const hasInitializedOnce = ref(false)

  // 缓存的数据
  const cachedUiSettings = ref<UiSettings | null>(null)
  const cachedDownloadersList = ref<Downloader[]>([])
  const cachedAllDownloadersList = ref<Downloader[]>([])
  const cachedAllSourceSitesStatus = ref<SiteStatus[]>([])

  // 数据加载状态
  const isUiSettingsLoaded = ref(false)
  const isDownloadersLoaded = ref(false)
  const isSitesStatusLoaded = ref(false)

  const setInitialized = () => {
    hasInitializedOnce.value = true
  }

  // 更新 UI 设置缓存（用于页面内状态变化后的即时同步）
  const updateCachedUiSettings = (settings: UiSettings) => {
    cachedUiSettings.value = cloneUiSettings(settings)
    isUiSettingsLoaded.value = true
  }

  // 获取 UI 设置（带缓存）
  const fetchUiSettings = async (forceRefresh = false): Promise<UiSettings> => {
    if (!forceRefresh && isUiSettingsLoaded.value && cachedUiSettings.value) {
      return cachedUiSettings.value
    }

    try {
      const response = await axios.get('/api/ui_settings')
      cachedUiSettings.value = response.data
      isUiSettingsLoaded.value = true
      return cachedUiSettings.value!
    } catch (e) {
      console.error('加载UI设置时出错:', e)
      isUiSettingsLoaded.value = true // 即使失败也标记为已加载，避免重复请求
      return cachedUiSettings.value || {}
    }
  }

  // 获取下载器列表（带缓存）
  const fetchDownloadersList = async (forceRefresh = false): Promise<{
    downloadersList: Downloader[]
    allDownloadersList: Downloader[]
  }> => {
    if (!forceRefresh && isDownloadersLoaded.value) {
      return {
        downloadersList: cachedDownloadersList.value,
        allDownloadersList: cachedAllDownloadersList.value,
      }
    }

    try {
      const response = await axios.get('/api/all_downloaders')
      const allDownloaders = response.data
      cachedDownloadersList.value = allDownloaders.filter((d: Downloader) => d.enabled)
      cachedAllDownloadersList.value = allDownloaders
      isDownloadersLoaded.value = true
      return {
        downloadersList: cachedDownloadersList.value,
        allDownloadersList: cachedAllDownloadersList.value,
      }
    } catch (e) {
      console.error('获取下载器列表时出错:', e)
      isDownloadersLoaded.value = true
      return {
        downloadersList: cachedDownloadersList.value,
        allDownloadersList: cachedAllDownloadersList.value,
      }
    }
  }

  // 获取站点状态（带缓存）
  const fetchSitesStatus = async (forceRefresh = false): Promise<SiteStatus[]> => {
    if (!forceRefresh && isSitesStatusLoaded.value) {
      return cachedAllSourceSitesStatus.value
    }

    try {
      const response = await axios.get('/api/sites/status')
      const allSites = response.data
      cachedAllSourceSitesStatus.value = allSites.filter((s: SiteStatus) => s.is_source)
      isSitesStatusLoaded.value = true
      return cachedAllSourceSitesStatus.value
    } catch (e) {
      console.error('获取站点状态时出错:', e)
      isSitesStatusLoaded.value = true
      return cachedAllSourceSitesStatus.value
    }
  }

  // 强制刷新所有缓存数据
  const refreshAllCachedData = async () => {
    await Promise.all([
      fetchUiSettings(true),
      fetchDownloadersList(true),
      fetchSitesStatus(true),
    ])
  }

  return {
    // 状态
    hasInitializedOnce,
    cachedUiSettings,
    cachedDownloadersList,
    cachedAllDownloadersList,
    cachedAllSourceSitesStatus,
    isUiSettingsLoaded,
    isDownloadersLoaded,
    isSitesStatusLoaded,
    // 方法
    setInitialized,
    updateCachedUiSettings,
    fetchUiSettings,
    fetchDownloadersList,
    fetchSitesStatus,
    refreshAllCachedData,
  }
})
