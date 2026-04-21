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

    <div v-if="ready" class="seed-maintenance-view__header glass-table">
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
          :disabled="loading"
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
          :disabled="loading"
        />
        <el-button type="info" plain @click="openRecordViewDialog">BDInfo记录</el-button>
        <el-button type="warning" plain @click="openBatchFetchDialog">获取数据</el-button>
        <el-button plain @click="handleBack">返回列表</el-button>
        <el-button type="primary" plain :loading="loading" @click="loadSeedInfo">
          加载种子
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

    <div v-else class="seed-maintenance-view__header glass-table">
      <div class="seed-maintenance-view__title-block">
        <h2 class="seed-maintenance-view__title">维护种子信息</h2>
        <p class="seed-maintenance-view__subtitle">
          默认展示待维护列表，可按路径筛选后进入单条维护。
        </p>
      </div>
    </div>

    <div v-if="loading" class="seed-maintenance-view__loading glass-table" v-loading="true">
      <div class="seed-maintenance-view__loading-text">正在加载种子信息...</div>
    </div>

    <div v-else-if="ready" class="seed-maintenance-view__panel glass-table">
      <CrossSeedPanel
        :show-complete-button="true"
        publish-scene="maintenance"
        :prefetched-db-seed-info="prefetchedDbSeedInfo"
        @complete="handleComplete"
        @cancel="handleBack"
      />
    </div>

    <div v-else class="seed-maintenance-view__list-panel">
      <CrossSeedDataView mode="maintenance" @maintain="handleMaintainRow" @ready="emitReady" />
    </div>

    <BDInfoRecordsDialog v-model="recordDialogVisible" @closed="handleRecordDialogClosed" />

    <div v-if="batchFetchDialogVisible" class="seed-maintenance-view__overlay">
      <el-card class="seed-maintenance-view__modal" shadow="always">
        <template #header>
          <div class="seed-maintenance-view__modal-header">
            <span>批量获取种子数据</span>
            <el-button type="danger" circle plain @click="closeBatchFetchDialog">X</el-button>
          </div>
        </template>
        <div class="seed-maintenance-view__modal-body">
          <BatchFetchPanel
            @cancel="closeBatchFetchDialog"
            @fetch-completed="handleFetchCompleted"
          />
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import BatchFetchPanel from '@/components/BatchFetchPanel.vue'
import CrossSeedPanel from '@/components/CrossSeedPanel.vue'
import BDInfoRecordsDialog from '@/components/cross-seed-data/BDInfoRecordsDialog.vue'
import CrossSeedDataView from '@/views/CrossSeedDataView.vue'
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

interface MaintenanceRow {
  torrent_id: string
  site_name: string
  nickname?: string
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
const recordDialogVisible = ref(false)
const batchFetchDialogVisible = ref(false)

const routeTorrentId = computed(() => String(route.query.torrent_id || '').trim())
const routeSiteName = computed(() => String(route.query.site_name || '').trim())
const rowId = computed(() => String(route.query.row_id || '').trim())
const sourcePage = computed(() => String(route.query.from || '/data').trim() || '/data')
const currentMaintenanceRow = ref<MaintenanceRow | null>(null)

const ready = computed(() => !!crossSeedStore.taskId && !!prefetchedDbSeedInfo.value)
const activeTargetTorrentId = computed(
  () => currentMaintenanceRow.value?.torrent_id?.trim() || routeTorrentId.value,
)
const activeTargetSiteName = computed(
  () => currentMaintenanceRow.value?.site_name?.trim() || routeSiteName.value,
)
const activeRowId = computed(
  () => currentMaintenanceRow.value?.nickname?.trim() || rowId.value || '',
)
const canRefetch = computed(
  () =>
    !!selectedSourceSite.value.trim() &&
    !!sourceTorrentId.value.trim() &&
    !!prefetchedDbSeedInfo.value &&
    !!activeTargetTorrentId.value &&
    !!activeTargetSiteName.value,
)

const resolveSiteCode = (siteLabel: string) => {
  const trimmed = siteLabel.trim()
  if (!trimmed) return ''
  const matched = sourceSiteOptions.value.find((item) => item.name === trimmed)
  return matched?.site || trimmed.toLowerCase()
}

const loadSourceSites = async () => {
  const sites = await torrentsViewState.fetchSitesStatus()
  sourceSiteOptions.value = sites.map((site) => ({
    name: String(site.name || '').trim(),
    site: String(site.site || '').trim(),
  }))
}

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

const applySeedInfo = (seedInfoResult: Record<string, any>) => {
  prefetchedDbSeedInfo.value = seedInfoResult
  crossSeedStore.reset()
  crossSeedStore.setParams(buildWorkingTorrent(seedInfoResult.data))

  const fallbackSourceName = selectedSourceSite.value || seedInfoResult.data.site_name
  const sourceInfo: SourceInfo = {
    name: fallbackSourceName,
    site: resolveSiteCode(fallbackSourceName),
    torrentId: sourceTorrentId.value || seedInfoResult.data.torrent_id,
  }
  crossSeedStore.setSourceInfo(sourceInfo)

  const idSuffix = activeRowId.value || `${seedInfoResult.data.site_name}_${seedInfoResult.data.torrent_id}`
  crossSeedStore.setTaskId(`seed_maintenance_${idSuffix}_${Date.now()}`)
}

const loadSeedInfo = async () => {
  const effectiveTorrentId = sourceTorrentId.value.trim() || routeTorrentId.value
  const effectiveSiteName = selectedSourceSite.value.trim() || routeSiteName.value

  if (!effectiveTorrentId || !effectiveSiteName) {
    error.value = '请选择源站并输入源站种子 ID 后再加载维护页'
    return
  }

  loading.value = true
  error.value = ''
  prefetchedDbSeedInfo.value = undefined
  crossSeedStore.reset()

  try {
    const response = await axios.get(
      `/api/migrate/get_db_seed_info?torrent_id=${encodeURIComponent(effectiveTorrentId)}&site_name=${encodeURIComponent(effectiveSiteName)}`,
    )
    const result = response.data

    if (!result.success) {
      throw new Error(result.error || '获取种子参数失败')
    }

    selectedSourceSite.value = effectiveSiteName
    sourceTorrentId.value = effectiveTorrentId
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
  if (!canRefetch.value || !prefetchedDbSeedInfo.value) {
    return
  }

  refetching.value = true
  error.value = ''

  try {
    const prefetchedSeedData = (prefetchedDbSeedInfo.value as any)?.data || {}
    const payload = {
      sourceSite: selectedSourceSite.value.trim(),
      searchTerm: sourceTorrentId.value.trim(),
      savePath: String(prefetchedSeedData.save_path || ''),
      torrentName: String(prefetchedSeedData.name || prefetchedSeedData.title || ''),
      downloaderId: String(prefetchedSeedData.downloader_id || ''),
      target_torrent_id: activeTargetTorrentId.value,
      target_site_name: activeTargetSiteName.value,
      screenshotReviewMode: 'interactive',
      task_id: crossSeedStore.taskId || undefined,
    }
    const response = await axios.post('/api/migrate/refetch_maintenance_seed', payload)
    const result = response.data
    if (!result.success) {
      throw new Error(result.message || result.error || '重新拉取失败')
    }

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

const openRecordViewDialog = () => {
  recordDialogVisible.value = true
}

const handleRecordDialogClosed = async () => {
  if (routeTorrentId.value && routeSiteName.value) {
    await loadSeedInfo()
  }
}

const openBatchFetchDialog = () => {
  batchFetchDialogVisible.value = true
}

const closeBatchFetchDialog = () => {
  batchFetchDialogVisible.value = false
}

const handleFetchCompleted = async () => {
  batchFetchDialogVisible.value = false
  ElMessage.success('批量获取种子数据已完成')
  if (activeTargetTorrentId.value && activeTargetSiteName.value) {
    await loadSeedInfo()
  }
}

const handleMaintainRow = async (row: MaintenanceRow) => {
  currentMaintenanceRow.value = {
    torrent_id: String(row.torrent_id || '').trim(),
    site_name: String(row.site_name || '').trim(),
    nickname: String(row.nickname || '').trim(),
  }
  selectedSourceSite.value = currentMaintenanceRow.value.site_name
  sourceTorrentId.value = currentMaintenanceRow.value.torrent_id
  await loadSeedInfo()
}

const emitReady = (refreshMethod: () => Promise<void>) => {
  emit('ready', refreshMethod)
}

const handleBack = () => {
  prefetchedDbSeedInfo.value = undefined
  currentMaintenanceRow.value = null
  crossSeedStore.reset()
  if (route.query.torrent_id || route.query.site_name || route.query.from) {
    void router.push(sourcePage.value)
    return
  }
  void router.push('/seed-maintenance')
}

const handleComplete = () => {
  ElMessage.success('种子信息维护已完成！')
  prefetchedDbSeedInfo.value = undefined
  currentMaintenanceRow.value = null
  crossSeedStore.reset()
  void router.push(sourcePage.value)
}

onMounted(async () => {
  await loadSourceSites()
  if (routeSiteName.value) {
    selectedSourceSite.value = routeSiteName.value
  }
  if (routeTorrentId.value) {
    sourceTorrentId.value = routeTorrentId.value
  }
  if (routeTorrentId.value && routeSiteName.value) {
    currentMaintenanceRow.value = {
      torrent_id: routeTorrentId.value,
      site_name: routeSiteName.value,
      nickname: rowId.value,
    }
    await loadSeedInfo()
  }
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

.seed-maintenance-view__panel,
.seed-maintenance-view__list-panel {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.seed-maintenance-view__panel :deep(.cross-seed-panel) {
  height: 100%;
}

.seed-maintenance-view__list-panel :deep(.cross-seed-data-view) {
  height: 100%;
}

.seed-maintenance-view__overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  z-index: 2000;
}

.seed-maintenance-view__modal {
  width: min(1100px, 100%);
  max-height: calc(100vh - 48px);
  display: flex;
  flex-direction: column;
}

.seed-maintenance-view__modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.seed-maintenance-view__modal-body {
  min-height: 0;
  overflow: auto;
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
