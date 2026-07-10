import axios from 'axios'
import { defineStore } from 'pinia'
import { ref } from 'vue'

type SettingsResponse = {
  ui_settings?: {
    content_filters?: {
      video_only?: boolean
    }
  }
}

export const useContentFiltersStore = defineStore('contentFilters', () => {
  const videoOnly = ref(false)
  const loaded = ref(false)
  let loadingPromise: Promise<void> | null = null

  const load = async (force = false) => {
    if (!force && loaded.value) return
    if (!force && loadingPromise) return loadingPromise

    loadingPromise = axios
      .get<SettingsResponse>('/api/settings')
      .then((response) => {
        videoOnly.value = response.data.ui_settings?.content_filters?.video_only === true
      })
      .catch(() => {
        videoOnly.value = false
      })
      .finally(() => {
        loaded.value = true
        loadingPromise = null
      })
    return loadingPromise
  }

  const setVideoOnly = (enabled: boolean) => {
    videoOnly.value = enabled
    loaded.value = true
  }

  const appendQuery = (params: URLSearchParams) => {
    params.set('video_only', videoOnly.value ? '1' : '0')
  }

  return { videoOnly, loaded, load, setVideoOnly, appendQuery }
})
