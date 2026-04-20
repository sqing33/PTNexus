<template>
  <div class="seed-maintenance-view">
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      style="margin-bottom: 16px"
    />

    <div class="seed-maintenance-view__header glass-table">
      <div class="seed-maintenance-view__title-block">
        <h2 class="seed-maintenance-view__title">维护种子信息</h2>
        <p class="seed-maintenance-view__subtitle">
          独立维护当前种子的元数据与发布参数，不在这里执行批量筛选。
        </p>
      </div>
      <div class="seed-maintenance-view__actions">
        <el-select
          v-model="selectedSourceSite"
          class="seed-maintenance-view__source-select"
          placeholder="选择源站"
          filterable
          :disabled="loading || refetching"
        >
          <el-option
            v-for="site in sourceSiteOptions"
            :key="site.site"
            :label="site.name"
            :value="site.name"
          />
        </el-select>
        <el-input
          v-model="sourceTorrentId"
          class="seed-maintenance-view__source-id"
          placeholder="源站种子 ID"
          clearable
          :disabled="loading || refetching"
        />
        <el-button plain @click="handleBack">返回一站多种</el-button>
        <el-button
          type="primary"
          plain
          :loading="loading"
          :disabled="refetching"
          @click="loadSeedInfo"
        >
          重新加载
        </el-button>
        <el-button
          type="warning"
          plain
          :loading="refetching"
          :disabled="loading || !canRefetch"
          @click="refetchSeedInfo"
        >
          重新拉取数据
        </el-button>
      </div>
    </div>

    <div v-if="loading" class="seed-maintenance-view__loading glass-table" v-loading="true">
      <div class="seed-maintenance-view__loading-text">正在加载种子信息...</div>
    </div>

    <div v-else-if="ready" class="seed-maintenance-view__panel glass-table">
      <CrossSeedPanel
        :show-complete-button="true"
        publish-scene="multi_torrent"
        :prefetched-db-seed-info="prefetchedDbSeedInfo"
        @complete="handleComplete"
        @cancel="handleBack"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import CrossSeedPanel from '@/components/CrossSeedPanel.vue'
import { useCrossSeedStore } from '@/stores/crossSeed'
import { useTorrentsViewState } from '@/stores/torrentsViewState'
import { ElMessage } from '@/utils/uiNotify'
import '@/assets/styles/glass-morphism.scss'

interface SourceInfo {
  name: string
  site: string
  torrentId: string
}

interface SourceSiteOption {
  name: string
  site: string
}

const emit = defineEmits<{
  (e: 'ready', refreshMethod: () => Promise<void>): void
}>()

const router = useRouter()
const route = useRoute()
const crossSeedStore = useCrossSeedStore()
const torrentsViewState = useTorrentsViewState()

const loading = ref(false)
const refetching = ref(false)
const error = ref('')
const prefetchedDbSeedInfo = ref<Record<string, unknown> | undefined>(undefined)
const sourceSiteOptions = ref<SourceSiteOption[]>([])
const selectedSourceSite = ref('')
const sourceTorrentId = ref('')

const torrentId = computed(() => String(route.query.torrent_id || '').trim())
const siteName = computed(() => String(route.query.site_name || '').trim())
const rowId = computed(() => String(route.query.row_id || '').trim())
const sourcePage = computed(() => String(route.query.from || '/data').trim() || '/data')

const ready = computed(() => !!crossSeedStore.taskId && !!prefetchedDbSeedInfo.value)
const canRefetch = computed(
  () =>
    !!selectedSourceSite.value.trim() &&
    !!sourceTorrentId.value.trim() &&
    !!prefetchedDbSeedInfo.value,
)

const buildWorkingTorrent = (seedInfo: Record<string, any>) => ({
  ...seedInfo,
  name: seedInfo.name || seedInfo.title,
  save_path: seedInfo.save_path || '',
  size: 0,
  size_formatted: '0 B',
  progress: 100,
  state: 'completed',
  total_uploaded: 0,
  total_uploaded_formatted: '0 B',
  downloaderId: seedInfo.downloader_id || null,
  sites: {
    [seedInfo.site_name]: {
      torrentId: seedInfo.torrent_id,
      site: seedInfo.site_name,
      site_name: seedInfo.site_name,
      comment: `id=${seedInfo.torrent_id}`,
    },
  },
})

const getEnglishSiteName = (siteLabel: string) => {
  const trimmed = siteLabel.trim()
  if (!trimmed) return ''
  const matched = sourceSiteOptions.value.find((item) => item.name === trimmed)
  return matched?.site || trimmed.toLowerCase()
}

const applySeedInfo = (seedInfoResult: Record<string, any>) => {
  prefetchedDbSeedInfo.value = seedInfoResult
  crossSeedStore.reset()
  crossSeedStore.setParams(buildWorkingTorrent(seedInfoResult.data))

  const fallbackSourceName = selectedSourceSite.value || seedInfoResult.data.site_name
  const sourceInfo: SourceInfo = {
    name: fallbackSourceName,
    site: getEnglishSiteName(fallbackSourceName),
    torrentId: sourceTorrentId.value || seedInfoResult.data.torrent_id,
  }
  crossSeedStore.setSourceInfo(sourceInfo)

  const idSuffix = rowId.value || `${seedInfoResult.data.site_name}_${seedInfoResult.data.torrent_id}`
  crossSeedStore.setTaskId(`seed_maintenance_${idSuffix}_${Date.now()}`)
}

const loadSourceSites = async () => {
  const sites = await torrentsViewState.fetchSitesStatus()
  sourceSiteOptions.value = sites.map((site) => ({
    name: String(site.name || '').trim(),
    site: String(site.site || '').trim(),
  }))
}

const loadSeedInfo = async () => {
  if (!torrentId.value || !siteName.value) {
    error.value = '缺少 torrent_id 或 site_name，无法进入维护页'
    return
  }

  loading.value = true
  error.value = ''
  prefetchedDbSeedInfo.value = undefined
  crossSeedStore.reset()

  try {
    const response = await axios.get(
      `/api/migrate/get_db_seed_info?torrent_id=${encodeURIComponent(torrentId.value)}&site_name=${encodeURIComponent(siteName.value)}`,
    )
    const result = response.data

    if (!result.success) {
      throw new Error(result.error || '获取种子参数失败')
    }

    applySeedInfo(result)
  } catch (err: unknown) {
    const message = axios.isAxiosError(err)
      ? (err.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (err.response?.data as { error?: string } | undefined)?.error ||
        err.message
      : err instanceof Error
        ? err.message
        : '网络错误'
    error.value = message
    ElMessage.error(message)
  } finally {
    loading.value = false
  }
}

const refetchSeedInfo = async () => {
  if (!prefetchedDbSeedInfo.value || !canRefetch.value) {
    return
  }

  refetching.value = true
  error.value = ''

  try {
    const prefetchedSeedData = (prefetchedDbSeedInfo.value as any)?.data || {}
    const payload = {
      sourceSite: selectedSourceSite.value,
      searchTerm: sourceTorrentId.value,
      savePath: String(prefetchedSeedData.save_path || ''),
      torrentName: String(prefetchedSeedData.name || prefetchedSeedData.title || ''),
      downloaderId: String(prefetchedSeedData.downloader_id || ''),
      target_torrent_id: torrentId.value,
      target_site_name: siteName.value,
      screenshotReviewMode: 'interactive',
      task_id: crossSeedStore.taskId || undefined,
    }
    const response = await axios.post('/api/migrate/refetch_maintenance_seed', payload)
    const result = response.data
    if (!result.success) {
      throw new Error(result.message || result.error || '重新拉取失败')
    }
    sourceTorrentId.value = String(sourceTorrentId.value).trim()
    applySeedInfo(result)
    ElMessage.success('重新拉取完成')
  } catch (err: unknown) {
    const message = axios.isAxiosError(err)
      ? (err.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (err.response?.data as { error?: string } | undefined)?.error ||
        err.message
      : err instanceof Error
        ? err.message
        : '网络错误'
    error.value = message
    ElMessage.error(message)
  } finally {
    refetching.value = false
  }
}

const handleBack = () => {
  prefetchedDbSeedInfo.value = undefined
  crossSeedStore.reset()
  void router.push(sourcePage.value)
}

const handleComplete = () => {
  ElMessage.success('种子信息维护已完成！')
  prefetchedDbSeedInfo.value = undefined
  crossSeedStore.reset()
  void router.push(sourcePage.value)
}

onMounted(async () => {
  await loadSourceSites()
  selectedSourceSite.value = siteName.value
  sourceTorrentId.value = torrentId.value
  await loadSeedInfo()
  emit('ready', loadSeedInfo)
})
</script>

<style scoped>
.seed-maintenance-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
}

.seed-maintenance-view__header,
.seed-maintenance-view__loading,
.seed-maintenance-view__panel {
  border-radius: 16px;
  padding: 20px;
}

.seed-maintenance-view__header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.seed-maintenance-view__title {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
}

.seed-maintenance-view__subtitle {
  margin: 6px 0 0;
  color: var(--el-text-color-secondary);
}

.seed-maintenance-view__actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
}

.seed-maintenance-view__source-select {
  width: 180px;
}

.seed-maintenance-view__source-id {
  width: 180px;
}

.seed-maintenance-view__loading {
  min-height: 240px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.seed-maintenance-view__loading-text {
  color: var(--el-text-color-secondary);
}

.seed-maintenance-view__panel {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.seed-maintenance-view__panel :deep(.cross-seed-panel) {
  height: 100%;
}

@media (max-width: 768px) {
  .seed-maintenance-view__header {
    flex-direction: column;
    align-items: stretch;
  }

  .seed-maintenance-view__actions {
    justify-content: stretch;
  }

  .seed-maintenance-view__source-select,
  .seed-maintenance-view__source-id {
    width: 100%;
  }
}
</style>
