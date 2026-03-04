<template>
  <div class="publish-logs-view">
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      style="margin: 0; border-radius: 0"
    />

    <div class="search-and-controls glass-table">
      <el-input
        v-model="searchQuery"
        placeholder="搜索标题/副标题/种子ID/站点/触发/场景..."
        clearable
        class="search-input"
        style="width: 320px; margin-right: 15px"
        @keyup.enter="applyFilters"
      />

      <el-select
        v-model="sceneFilter"
        placeholder="场景"
        clearable
        style="width: 140px; margin-right: 15px"
      >
        <el-option label="一种多站" value="multi_site" />
        <el-option label="一站多种" value="multi_torrent" />
      </el-select>

      <el-select
        v-model="triggerFilter"
        placeholder="触发（如：批量转种-1）"
        clearable
        filterable
        allow-create
        default-first-option
        style="width: 180px; margin-right: 15px"
      >
        <el-option label="手动" value="manual" />
        <el-option label="队列" value="queue" />
      </el-select>

      <el-input
        v-model="queueGroupFilter"
        placeholder="队列分组ID"
        clearable
        style="width: 170px; margin-right: 15px"
        @keyup.enter="applyFilters"
      />

      <el-select
        v-model="statusFilter"
        placeholder="发布状态"
        clearable
        style="width: 160px; margin-right: 15px"
      >
        <el-option label="待发布" value="queued" />
        <el-option label="发布成功" value="success" />
        <el-option label="发布失败" value="failed" />
        <el-option label="已过滤" value="filtered" />
        <el-option label="已存在" value="exists" />
        <el-option label="已编辑" value="edited" />
        <el-option label="已取消" value="cancelled" />
        <el-option label="预检查限制" value="pre_check_limit" />
      </el-select>

      <el-input
        v-model="targetSiteFilter"
        placeholder="目标站点"
        clearable
        style="width: 140px; margin-right: 15px"
        @keyup.enter="applyFilters"
      />

      <el-button type="primary" plain @click="applyFilters">查询</el-button>
      <el-button type="danger" plain style="margin-left: 8px" @click="clearFilters">清空</el-button>

      <div class="pagination-controls" v-if="total > 0">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
          background
        />
      </div>
    </div>

    <div class="table-container">
      <el-table
        :data="rows"
        row-key="id"
        v-loading="loading"
        border
        style="width: 100%"
        height="100%"
        empty-text="暂无发种日志"
        class="glass-table"
      >
        <el-table-column label="加入时间" width="150" align="center">
          <template #default="scope">
            <div class="datetime-cell">{{ formatDateTimeTwoLines(scope.row.created_at) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="150" align="center">
          <template #default="scope">
            <div class="datetime-cell">{{ formatDateTimeTwoLines(scope.row.updated_at) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="场景" width="110" align="center">
          <template #default="scope">
            {{ formatScene(scope.row.scene) }}
          </template>
        </el-table-column>
        <el-table-column label="触发" width="120" align="center">
          <template #default="scope">
            {{ formatTrigger(scope.row.trigger) }}
          </template>
        </el-table-column>
        <el-table-column prop="source_site" label="源站" width="90" align="center" />
        <el-table-column prop="target_site" label="目标站" width="90" align="center" />
        <el-table-column prop="torrent_id" label="ID" width="90" align="center" show-overflow-tooltip />

        <el-table-column label="标题" min-width="360">
          <template #default="scope">
            <div class="title-cell">
              <div class="subtitle-line" :title="scope.row.subtitle">{{ scope.row.subtitle || '' }}</div>
              <div class="main-title-line" :title="scope.row.title">{{ scope.row.title || '' }}</div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="发布状态" width="120" align="center">
          <template #default="scope">
            <div class="status-tags">
              <el-tag :type="publishStatusTagType(scope.row.status)" size="small">
                {{ formatPublishStatus(scope.row.status) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="下载器" width="120" align="center">
          <template #default="scope">
            <div class="status-tags">
              <el-tag
                v-if="scope.row.status !== 'queued' && scope.row.status !== 'cancelled'"
                :type="downloaderTagType(scope.row)"
                size="small"
                :style="downloaderTagStyle(scope.row)"
              >
                {{ formatDownloaderStatus(scope.row) }}
              </el-tag>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="170" align="center" fixed="right">
          <template #default="scope">
            <div class="action-buttons">
              <el-button size="small" type="primary" @click="openLogs(scope.row)">日志</el-button>
              <el-button
                size="small"
                type="success"
                style="margin-left: 5px"
                :disabled="!scope.row.result_url"
                @click="openResultURL(scope.row)"
              >
                打开
              </el-button>
              <el-button
                size="small"
                type="danger"
                style="margin-left: 5px"
                :disabled="!canDeleteQueued(scope.row)"
                @click="deleteQueuedTask(scope.row)"
              >
                删除
              </el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <LogViewerCard v-model="dialogVisible" :title="dialogTitle" :content="dialogContent" />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import axios from 'axios'
import { useTorrentsViewState } from '@/stores/torrentsViewState'
import LogViewerCard from '@/components/LogViewerCard.vue'
import type { Downloader } from '@/types'
import { ElMessage } from '@/utils/uiNotify'

const emits = defineEmits(['ready'])

type PublishLogRow = {
  id: number | string
  created_at: string
  updated_at: string
  scene: string
  trigger: string
  source_site: string
  target_site: string
  torrent_id: number | string
  title?: string | null
  subtitle?: string | null
  status: string
  result_url?: string | null
  queue_task_id?: number | string | null
  logs?: string | null
  auto_add_result?: string | null
  [key: string]: unknown
}

type ParsedAutoAddResult = {
  success: boolean
  message: string
  downloaderName: string
  downloaderId: string
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null

const loading = ref(false)
const error = ref('')

const torrentsViewState = useTorrentsViewState()
const allDownloadersList = ref<Downloader[]>([])
const route = useRoute()
const router = useRouter()

const rows = ref<PublishLogRow[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)

const searchQuery = ref('')
const statusFilter = ref('')
const triggerFilter = ref('')
const sceneFilter = ref('')
const queueGroupFilter = ref('')
const targetSiteFilter = ref('')

const dialogVisible = ref(false)
const dialogTitle = ref('日志')
const dialogContent = ref('')

const POLL_INTERVAL_MS = 3000
let pollTimer: ReturnType<typeof setInterval> | null = null
let pollRefreshing = false
let fetchSeq = 0

const readStringSetting = (value: unknown) => {
  if (typeof value === 'string') return value
  if (value === undefined || value === null) return ''
  return String(value)
}

const buildUiSettings = () => ({
  page_size: pageSize.value,
  search_query: searchQuery.value,
  active_filters: {
    status: statusFilter.value,
    trigger: triggerFilter.value,
    scene: sceneFilter.value,
    queue_group_id: queueGroupFilter.value,
    target_site: targetSiteFilter.value,
  },
})

const loadUiSettings = async () => {
  try {
    const response = await axios.get('/api/ui_settings/publish_logs')
    const settings = response.data
    const loadedPageSize = Number(settings?.page_size)
    pageSize.value = Number.isFinite(loadedPageSize) && loadedPageSize > 0 ? loadedPageSize : 20
    searchQuery.value = readStringSetting(settings?.search_query)

    const activeFilters = settings?.active_filters
    if (isRecord(activeFilters)) {
      statusFilter.value = readStringSetting(activeFilters.status)
      triggerFilter.value = readStringSetting(activeFilters.trigger)
      sceneFilter.value = readStringSetting(activeFilters.scene)
      queueGroupFilter.value = readStringSetting(activeFilters.queue_group_id)
      targetSiteFilter.value = readStringSetting(activeFilters.target_site)
    }
  } catch (e) {
    console.error('加载发布日志 UI 设置时出错:', e)
  }
}

const saveUiSettings = async () => {
  try {
    await axios.post('/api/ui_settings/publish_logs', buildUiSettings())
  } catch (e) {
    console.error('无法保存发布日志 UI 设置:', e)
  }
}

const publishStatusTagType = (status: string) => {
  if (status === 'queued') return 'info'
  if (status === 'success') return 'success'
  if (status === 'edited') return 'success'
  if (status === 'exists') return 'warning'
  if (status === 'filtered') return 'warning'
  if (status === 'pre_check_limit') return 'danger'
  if (status === 'cancelled') return 'info'
  if (status === 'failed') return 'danger'
  return 'info'
}

const formatPublishStatus = (status: string) => {
  if (status === 'queued') return '待发布'
  if (status === 'success') return '发布成功'
  if (status === 'failed') return '发布失败'
  if (status === 'filtered') return '已过滤'
  if (status === 'exists') return '已存在'
  if (status === 'edited') return '已编辑'
  if (status === 'pre_check_limit') return '预检查限制'
  if (status === 'cancelled') return '已取消'
  return status || '未知'
}

const formatTrigger = (trigger: string) => {
  if (trigger === 'queue') return '队列'
  if (trigger === 'manual') return '手动'
  return trigger || '未知'
}

const formatScene = (scene: string) => {
  if (scene === 'multi_site') return '一种多站'
  if (scene === 'multi_torrent') return '一站多种'
  return scene || '未知'
}

const formatDateTimeTwoLines = (raw: string) => {
  const trimmed = (raw || '').trim()
  if (!trimmed) return '-\n-'

  const parts = trimmed.split(' ')
  if (parts.length >= 2) {
    const datePart = parts[0] || ''
    const timePart = parts[1] || ''
    return `${datePart}\n${timePart}`
  }

  const dt = new Date(trimmed)
  if (!Number.isNaN(dt.getTime())) {
    const yyyy = dt.getFullYear()
    const mm = String(dt.getMonth() + 1).padStart(2, '0')
    const dd = String(dt.getDate()).padStart(2, '0')
    const hh = String(dt.getHours()).padStart(2, '0')
    const mi = String(dt.getMinutes()).padStart(2, '0')
    const ss = String(dt.getSeconds()).padStart(2, '0')
    return `${yyyy}-${mm}-${dd}\n${hh}:${mi}:${ss}`
  }

  return trimmed
}

const parseAutoAddResult = (row: PublishLogRow | null | undefined): ParsedAutoAddResult => {
  const raw = String(row?.auto_add_result || '').trim()
  if (!raw) return { success: false, message: '', downloaderName: '', downloaderId: '' }
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!isRecord(parsed)) {
      return { success: false, message: raw, downloaderName: '', downloaderId: '' }
    }
    return {
      success: parsed.success === true,
      message: typeof parsed.message === 'string' ? parsed.message : String(parsed.message || ''),
      downloaderName:
        typeof parsed.downloader_name === 'string' ? parsed.downloader_name : String(parsed.downloader_name || ''),
      downloaderId: typeof parsed.downloader_id === 'string' ? parsed.downloader_id : String(parsed.downloader_id || ''),
    }
  } catch {
    return { success: false, message: raw, downloaderName: '', downloaderId: '' }
  }
}

const deriveDownloaderColor = (seed: string) => {
  const value = (seed || '').trim()
  if (!value) return ''

  let hash = 0
  for (let i = 0; i < value.length; i++) {
    hash = value.charCodeAt(i) + ((hash << 5) - hash)
  }

  const hue = (Math.abs(hash) % 320) + 20
  const saturation = 72
  const lightness = 45
  return `hsl(${hue} ${saturation}% ${lightness}%)`
}

const resolveDownloaderColor = (downloaderId: string, downloaderName: string) => {
  const id = (downloaderId || '').trim()
  if (id) {
    const item = allDownloadersList.value.find((d) => d.id === id)
    const configured = String(item?.color || '').trim()
    if (configured) return configured
    return deriveDownloaderColor(id)
  }

  const name = (downloaderName || '').trim()
  if (name) return deriveDownloaderColor(name)
  return ''
}

const isWaitingDownloaderStatus = (row: PublishLogRow, message: string) =>
  row.status === 'queued' || message.startsWith('等待发布')

const isSkippedDownloaderStatus = (row: PublishLogRow, message: string) =>
  row.status === 'cancelled' || message.startsWith('未执行')

const downloaderTagType = (row: PublishLogRow) => {
  const parsed = parseAutoAddResult(row)
  if (parsed.success) return 'info'
  if ((parsed.message || '').trim().startsWith('未执行')) return 'info'
  return 'info'
}

type DownloaderTagStyle = Record<string, string>

const downloaderTagStyle = (row: PublishLogRow): DownloaderTagStyle => {
  const parsed = parseAutoAddResult(row)
  const message = (parsed.message || '').trim()
  if (isWaitingDownloaderStatus(row, message) || isSkippedDownloaderStatus(row, message)) {
    return {} as DownloaderTagStyle
  }

  let color = ''
  if (parsed.success) {
    color = resolveDownloaderColor(parsed.downloaderId || '', parsed.downloaderName || '')
  } else {
    color = '#f56c6c'
  }

  if (!color) return {} as DownloaderTagStyle

  return {
    '--el-tag-bg-color': color,
    '--el-tag-border-color': color,
    '--el-tag-text-color': '#ffffff',
  }
}

const formatDownloaderStatus = (row: PublishLogRow) => {
  const parsed = parseAutoAddResult(row)
  const message = (parsed.message || '').trim()
  if (isWaitingDownloaderStatus(row, message)) return message || '等待发布'
  if (isSkippedDownloaderStatus(row, message)) return '未执行'
  if (parsed.success) {
    return parsed.downloaderName || parsed.downloaderId || '成功'
  }
  return '失败'
}

const fetchLogs = async (options: { silent?: boolean } = {}) => {
  const silent = options.silent === true
  const currentFetchSeq = ++fetchSeq
  if (!silent) {
    loading.value = true
    error.value = ''
  }
  try {
    const response = await axios.get('/api/publish_logs', {
      params: {
        page: currentPage.value,
        page_size: pageSize.value,
        search: searchQuery.value.trim(),
        status: statusFilter.value,
        trigger: triggerFilter.value,
        scene: sceneFilter.value,
        queue_group_id: queueGroupFilter.value.trim(),
        target_site: targetSiteFilter.value.trim(),
      },
    })

    if (!response.data?.success) {
      throw new Error(response.data?.message || '获取发种日志失败')
    }

    if (currentFetchSeq !== fetchSeq) return
    rows.value = Array.isArray(response.data.data) ? (response.data.data as PublishLogRow[]) : []
    total.value = Number(response.data.total || 0)
    if (error.value) {
      error.value = ''
    }
  } catch (e: unknown) {
    if (currentFetchSeq !== fetchSeq) return
    if (!silent) {
      const message = axios.isAxiosError(e)
        ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
          (e.response?.data as { error?: string } | undefined)?.error ||
          e.message)
        : e instanceof Error
          ? e.message
          : '获取发种日志失败'
      error.value = message
    }
  } finally {
    if (!silent && currentFetchSeq === fetchSeq) {
      loading.value = false
    }
  }
}

const applyFilters = async () => {
  currentPage.value = 1
  await fetchLogs()
  void saveUiSettings()
}

const clearFilters = async () => {
  searchQuery.value = ''
  statusFilter.value = ''
  triggerFilter.value = ''
  sceneFilter.value = ''
  queueGroupFilter.value = ''
  targetSiteFilter.value = ''
  currentPage.value = 1
  if (Object.keys(route.query || {}).length > 0) {
    await router.replace({ path: '/publish-logs', query: {} })
  }
  await fetchLogs()
  void saveUiSettings()
}

const handleSizeChange = async (size: number) => {
  pageSize.value = size
  currentPage.value = 1
  await fetchLogs()
  void saveUiSettings()
}

const handleCurrentChange = async (page: number) => {
  currentPage.value = page
  await fetchLogs()
}

const openLogs = (row: PublishLogRow) => {
  dialogTitle.value = `日志 - ${row.target_site || ''}`
  const base = row.logs || ''
  const parsed = parseAutoAddResult(row)
  const message = (parsed.message || '').trim()
  let addon = ''
  if (isWaitingDownloaderStatus(row, message)) {
    addon = `\n\n--- [下载器] ---\n${message || '等待发布'}`
  } else if (isSkippedDownloaderStatus(row, message)) {
    addon = '\n\n--- [下载器] ---\n未执行'
  } else if (parsed.success) {
    const name = parsed.downloaderName || parsed.downloaderId
    addon = `\n\n--- [下载器] ---\n成功${name ? `：${name}` : ''}`
  } else {
    addon = `\n\n--- [下载器] ---\n失败${message ? `：${message}` : ''}`
  }
  dialogContent.value = `${base}${addon}`.trim()
  dialogVisible.value = true
}

const openResultURL = (row: PublishLogRow) => {
  const url = String(row?.result_url || '').trim()
  if (!url) return
  window.open(url, '_blank', 'noopener,noreferrer')
}

const canDeleteQueued = (row: PublishLogRow) => {
  const status = String(row?.status || '').trim()
  const queueTaskID = Number(row?.queue_task_id || 0)
  return status === 'queued' && queueTaskID > 0
}

const deleteQueuedTask = async (row: PublishLogRow) => {
  if (!canDeleteQueued(row)) return
  const queueTaskID = Number(row.queue_task_id)
  try {
    await ElMessageBox.confirm('确认从队列移除该待发布任务？', '删除待发布任务', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
    })

    const response = await axios.delete(`/api/migrate/publish_queue/tasks/${queueTaskID}`)
    if (!response.data?.success) {
      throw new Error(response.data?.message || '删除失败')
    }

    ElMessage.success(response.data?.message || '队列任务已移除')
    await fetchLogs()
  } catch (error: unknown) {
    if (error === 'cancel' || error === 'close') return
    const message = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message)
      : error instanceof Error
        ? error.message
        : '删除失败'
    ElMessage.error(message)
  }
}

const readQuery = (key: string) => {
  const raw = route.query[key]
  if (Array.isArray(raw)) return String(raw[0] || '')
  if (raw === undefined || raw === null) return ''
  return String(raw)
}

const applyRouteFilters = () => {
  let changed = false

  const queryTrigger = readQuery('trigger').trim()
  const queryScene = readQuery('scene').trim()
  const queryStatus = readQuery('status').trim()
  const querySearch = readQuery('search').trim()
  const queryTargetSite = readQuery('target_site').trim()
  const queryQueueGroupID = readQuery('queue_group_id').trim()

  if (queryTrigger && triggerFilter.value !== queryTrigger) {
    triggerFilter.value = queryTrigger
    changed = true
  }
  if (queryScene && sceneFilter.value !== queryScene) {
    sceneFilter.value = queryScene
    changed = true
  }
  if (queryStatus && statusFilter.value !== queryStatus) {
    statusFilter.value = queryStatus
    changed = true
  }
  if (querySearch && searchQuery.value !== querySearch) {
    searchQuery.value = querySearch
    changed = true
  }
  if (queryTargetSite && targetSiteFilter.value !== queryTargetSite) {
    targetSiteFilter.value = queryTargetSite
    changed = true
  }
  if (queryQueueGroupID && queueGroupFilter.value !== queryQueueGroupID) {
    queueGroupFilter.value = queryQueueGroupID
    changed = true
  }

  return changed
}

const runPollRefresh = async () => {
  if (pollRefreshing || loading.value) return
  pollRefreshing = true
  try {
    await fetchLogs({ silent: true })
  } finally {
    pollRefreshing = false
  }
}

const startPolling = () => {
  if (pollTimer) {
    clearInterval(pollTimer)
  }
  pollTimer = setInterval(() => {
    void runPollRefresh()
  }, POLL_INTERVAL_MS)
}

const stopPolling = () => {
  if (!pollTimer) return
  clearInterval(pollTimer)
  pollTimer = null
}

onMounted(async () => {
  try {
    const result = await torrentsViewState.fetchDownloadersList(false)
    allDownloadersList.value = result.allDownloadersList || []
  } catch {
    allDownloadersList.value = []
  }

  await loadUiSettings()

  let routeFiltersApplied = false
  if (applyRouteFilters()) {
    currentPage.value = 1
    routeFiltersApplied = true
  }

  await fetchLogs()
  if (routeFiltersApplied) {
    void saveUiSettings()
  }
  emits('ready', fetchLogs)
  startPolling()
})

onBeforeUnmount(() => {
  stopPolling()
})

watch(
  () => route.query,
  async () => {
    if (applyRouteFilters()) {
      currentPage.value = 1
      await fetchLogs()
      void saveUiSettings()
    }
  },
)
</script>

<style scoped>
.publish-logs-view {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 0;
  box-sizing: border-box;
}

.search-and-controls {
  display: flex;
  align-items: center;
  padding: 10px 15px;
  background-color: #ffffff;
  border-bottom: 1px solid #ebeef5;
}

.pagination-controls {
  flex: 1;
  display: flex;
  justify-content: flex-end;
}

.table-container {
  flex: 1;
  overflow: hidden;
  min-height: 300px;
}

.table-container :deep(.el-table) {
  height: 100%;
}

.table-container :deep(.el-table__body-wrapper) {
  overflow-y: auto;
}

.table-container :deep(.el-table__header-wrapper) {
  overflow-x: hidden;
}

.datetime-cell {
  white-space: pre-line;
  line-height: 1.2;
  font-size: 12px;
}

.status-tags {
  display: flex;
  justify-content: center;
  align-items: center;
  width: 100%;
  height: 100%;
}

.action-buttons {
  display: flex;
  justify-content: center;
  align-items: center;
  width: 100%;
  height: 100%;
}

.title-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.subtitle-line {
  font-size: 12px;
  color: #909399;
  line-height: 1.2;
}
.main-title-line {
  font-size: 13px;
  line-height: 1.25;
  white-space: normal;
}
</style>
