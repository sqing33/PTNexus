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
        placeholder="搜索标题/副标题/种子ID/站点..."
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
        placeholder="触发方式"
        clearable
        style="width: 140px; margin-right: 15px"
      >
        <el-option label="手动" value="manual" />
        <el-option label="队列" value="queue" />
      </el-select>

      <el-select
        v-model="statusFilter"
        placeholder="发布状态"
        clearable
        style="width: 160px; margin-right: 15px"
      >
        <el-option label="待发布" value="queued" />
        <el-option label="发布成功" value="success" />
        <el-option label="发布失败" value="failed" />
        <el-option label="已存在" value="exists" />
        <el-option label="已编辑" value="edited" />
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
        <el-table-column label="触发" width="90" align="center">
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
              <div class="main-title-line" :title="scope.row.title">{{ scope.row.title || '' }}</div>
              <div class="subtitle-line" :title="scope.row.subtitle">{{ scope.row.subtitle || '' }}</div>
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
              <el-tag :type="downloaderTagType(scope.row)" size="small">
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
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="min(900px, calc(100vw - 32px))"
      align-center
      destroy-on-close
      append-to-body
      class="publish-log-dialog"
    >
      <pre class="log-pre">{{ dialogContent }}</pre>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import axios from 'axios'

const emits = defineEmits(['ready'])

const loading = ref(false)
const error = ref('')

const rows = ref<any[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)

const searchQuery = ref('')
const statusFilter = ref('')
const triggerFilter = ref('')
const sceneFilter = ref('')
const targetSiteFilter = ref('')

const dialogVisible = ref(false)
const dialogTitle = ref('日志')
const dialogContent = ref('')

const publishStatusTagType = (status: string) => {
  if (status === 'queued') return 'info'
  if (status === 'success') return 'success'
  if (status === 'edited') return 'success'
  if (status === 'exists') return 'warning'
  if (status === 'pre_check_limit') return 'warning'
  if (status === 'failed') return 'danger'
  return 'info'
}

const formatPublishStatus = (status: string) => {
  if (status === 'queued') return '待发布'
  if (status === 'success') return '发布成功'
  if (status === 'failed') return '发布失败'
  if (status === 'exists') return '已存在'
  if (status === 'edited') return '已编辑'
  if (status === 'pre_check_limit') return '预检查限制'
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

const parseAutoAddResult = (row: any) => {
  const raw = (row?.auto_add_result || '').trim()
  if (!raw) return { success: false, message: '' }
  try {
    const parsed = JSON.parse(raw)
    return {
      success: parsed?.success === true,
      message: String(parsed?.message || ''),
      downloaderName: String(parsed?.downloader_name || ''),
      downloaderId: String(parsed?.downloader_id || ''),
    }
  } catch {
    return { success: false, message: raw }
  }
}

const downloaderTagType = (row: any) => {
  const parsed = parseAutoAddResult(row)
  if (parsed.success) {
    const seed = (parsed.downloaderId || parsed.downloaderName || '').trim()
    if (!seed) return 'success'

    let hash = 0
    for (let i = 0; i < seed.length; i++) {
      hash = seed.charCodeAt(i) + ((hash << 5) - hash)
    }
    const types = ['primary', 'success', 'warning', 'info']
    return types[Math.abs(hash) % types.length]
  }
  return 'danger'
}

const formatDownloaderStatus = (row: any) => {
  const parsed = parseAutoAddResult(row)
  if (parsed.success) {
    return parsed.downloaderName || parsed.downloaderId || '成功'
  }
  return '失败'
}

const fetchLogs = async () => {
  loading.value = true
  error.value = ''
  try {
    const response = await axios.get('/api/publish_logs', {
      params: {
        page: currentPage.value,
        page_size: pageSize.value,
        search: searchQuery.value.trim(),
        status: statusFilter.value,
        trigger: triggerFilter.value,
        scene: sceneFilter.value,
        target_site: targetSiteFilter.value.trim(),
      },
    })

    if (!response.data?.success) {
      throw new Error(response.data?.message || '获取发种日志失败')
    }

    rows.value = response.data.data || []
    total.value = response.data.total || 0
  } catch (e: any) {
    error.value = e?.message || '获取发种日志失败'
  } finally {
    loading.value = false
  }
}

const applyFilters = async () => {
  currentPage.value = 1
  await fetchLogs()
}

const clearFilters = async () => {
  searchQuery.value = ''
  statusFilter.value = ''
  triggerFilter.value = ''
  sceneFilter.value = ''
  targetSiteFilter.value = ''
  currentPage.value = 1
  await fetchLogs()
}

const handleSizeChange = async (size: number) => {
  pageSize.value = size
  currentPage.value = 1
  await fetchLogs()
}

const handleCurrentChange = async (page: number) => {
  currentPage.value = page
  await fetchLogs()
}

const openLogs = (row: any) => {
  dialogTitle.value = `日志 - ${row.target_site || ''}`
  const base = row.logs || ''
  const parsed = parseAutoAddResult(row)
  let addon = ''
  if (parsed.success) {
    const name = parsed.downloaderName || parsed.downloaderId
    addon = `\n\n--- [下载器] ---\n成功${name ? `：${name}` : ''}`
  } else {
    const reason = (parsed.message || '').trim()
    addon = `\n\n--- [下载器] ---\n失败${reason ? `：${reason}` : ''}`
  }
  dialogContent.value = `${base}${addon}`.trim()
  dialogVisible.value = true
}

const openResultURL = (row: any) => {
  const url = String(row?.result_url || '').trim()
  if (!url) return
  window.open(url, '_blank', 'noopener,noreferrer')
}

onMounted(async () => {
  await fetchLogs()
  emits('ready', fetchLogs)
})
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
.log-pre {
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
  font-size: 13px;
  line-height: 1.4;
}

.publish-log-dialog :deep(.el-dialog) {
  max-height: 80vh;
  display: flex;
  flex-direction: column;
}

.publish-log-dialog :deep(.el-dialog__body) {
  overflow-y: auto;
  flex: 1;
}
</style>
