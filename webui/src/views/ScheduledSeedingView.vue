<template>
  <div class="scheduled-seeding-view">
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      style="margin: 0; border-radius: 0"
    />

    <div class="toolbar glass-table">
      <el-button type="primary" @click="openCreateDialog">新建任务</el-button>

      <el-select
        v-model="statusFilter"
        placeholder="状态筛选"
        clearable
        style="width: 140px; margin-left: 15px"
        @change="handleStatusChange"
      >
        <el-option label="全部" value="" />
        <el-option label="运行中" value="active" />
        <el-option label="已暂停" value="paused" />
        <el-option label="已完成" value="completed" />
      </el-select>

      <div class="batch-actions">
        <span class="batch-count" :class="{ 'batch-count-active': selectedRows.length > 0 }">
          已选 {{ selectedRows.length }} 项
        </span>
        <el-button
          size="small"
          type="success"
          :disabled="selectedRows.length === 0 || batchLoading"
          :loading="batchLoading"
          @click="batchSetStatus('active')"
        >
          批量启动
        </el-button>
        <el-button
          size="small"
          type="warning"
          :disabled="selectedRows.length === 0 || batchLoading"
          :loading="batchLoading"
          @click="batchSetStatus('paused')"
        >
          批量暂停
        </el-button>
        <el-button
          size="small"
          type="primary"
          :disabled="selectedRows.length === 0 || batchLoading"
          :loading="batchLoading"
          @click="batchTrigger"
        >
          批量发下一个
        </el-button>
        <el-popconfirm
          title="确认删除选中的任务？此操作不可恢复"
          confirm-button-text="删除"
          cancel-button-text="取消"
          @confirm="batchDelete"
        >
          <template #reference>
            <el-button
              size="small"
              type="danger"
              :disabled="selectedRows.length === 0 || batchLoading"
              :loading="batchLoading"
            >
              批量删除
            </el-button>
          </template>
        </el-popconfirm>
      </div>

      <div class="toolbar-spacer" />

      <div class="pagination-controls" v-if="total > 0">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[5, 10, 20, 50, 100]"
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
        ref="tableRef"
        :data="tasks"
        row-key="id"
        v-loading="loading"
        border
        style="width: 100%"
        height="100%"
        empty-text="暂无定时发种任务"
        class="glass-table"
        @expand-change="handleExpandChange"
        @selection-change="handleSelectionChange"
      >
        <el-table-column type="selection" width="45" />
        <el-table-column type="expand" width="40">
          <template #default="{ row }">
            <div class="expand-content">
              <el-descriptions :column="3" border size="small">
                <el-descriptions-item label="任务ID">{{ row.id }}</el-descriptions-item>
                <el-descriptions-item label="触发标签">{{ row.trigger_tag }}</el-descriptions-item>
                <el-descriptions-item label="循环发种">
                  <el-tag :type="row.loop_enabled ? 'success' : 'info'" size="small">
                    {{ row.loop_enabled ? '是' : '否' }}
                  </el-tag>
                </el-descriptions-item>
                <el-descriptions-item label="创建时间">{{ row.created_at }}</el-descriptions-item>
                <el-descriptions-item label="更新时间">{{ row.updated_at }}</el-descriptions-item>
                <el-descriptions-item label="上次执行">{{ formatDateTimeFull(row.last_run_at) }}</el-descriptions-item>
              </el-descriptions>
            </div>
          </template>
        </el-table-column>

        <el-table-column prop="name" label="任务名称" min-width="160" show-overflow-tooltip />

        <el-table-column label="种子数" width="80" align="center">
          <template #default="{ row }">
            {{ parseSeeds(row.seeds_json).length }}
          </template>
        </el-table-column>

        <el-table-column label="目标站点" min-width="180">
          <template #default="{ row }">
            <div class="site-tags">
              <el-tag
                v-for="site in parseTargetSites(row.target_sites_json)"
                :key="site"
                size="small"
                type="primary"
                effect="plain"
                class="site-tag"
              >
                {{ site }}
              </el-tag>
              <span v-if="parseTargetSites(row.target_sites_json).length === 0">-</span>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="间隔" width="100" align="center">
          <template #default="{ row }">
            {{ formatInterval(row.interval_minutes) }}
          </template>
        </el-table-column>

        <el-table-column label="状态" width="90" align="center">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">
              {{ formatStatus(row.status) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="进度" width="80" align="center">
          <template #default="{ row }">
            {{ row.current_seed_index }}/{{ parseSeeds(row.seeds_json).length }}
          </template>
        </el-table-column>

        <el-table-column label="已发布/跳过" width="110" align="center">
          <template #default="{ row }">
            <span class="stat-published">{{ row.total_published }}</span>
            <span class="stat-separator">/</span>
            <span class="stat-skipped">{{ row.total_skipped }}</span>
          </template>
        </el-table-column>

        <el-table-column label="下次执行" width="150" align="center">
          <template #default="{ row }">
            <div class="datetime-cell">{{ formatDateTime(row.next_run_at) }}</div>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="300" align="center" fixed="right">
          <template #default="{ row }">
            <div class="action-buttons">
              <el-button
                size="small"
                type="success"
                @click="triggerTask(row)"
                :disabled="row.status !== 'active'"
              >
                发下一个
              </el-button>
              <el-button
                size="small"
                :type="row.status === 'active' ? 'warning' : 'success'"
                @click="toggleTask(row)"
                :disabled="row.status === 'completed'"
              >
                {{ row.status === 'active' ? '暂停' : '启动' }}
              </el-button>
              <el-button size="small" type="primary" @click="openEditDialog(row)">编辑</el-button>
              <el-button size="small" type="info" @click="openLogsDrawer(row)">日志</el-button>
              <el-popconfirm
                title="确认删除该任务？"
                confirm-button-text="确定"
                cancel-button-text="取消"
                @confirm="deleteTask(row)"
              >
                <template #reference>
                  <el-button size="small" type="danger">删除</el-button>
                </template>
              </el-popconfirm>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <TaskFormDialog
      v-model:visible="formDialogVisible"
      :task="editingTask"
      @saved="handleTaskSaved"
    />

    <PublishLogsDrawer
      v-model:visible="logsDrawerVisible"
      :trigger-tag="activeTriggerTag"
    />
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage } from '@/utils/uiNotify'
import TaskFormDialog from '@/components/scheduled-seed/TaskFormDialog.vue'
import PublishLogsDrawer from '@/components/scheduled-seed/PublishLogsDrawer.vue'

const emits = defineEmits(['ready'])

type ScheduledSeedTask = {
  id: number
  name: string
  status: string
  seeds_json: string
  target_sites_json: string
  interval_minutes: number
  current_seed_index: number
  current_site_index: number
  total_published: number
  total_skipped: number
  loop_enabled: boolean
  trigger_tag: string
  last_run_at: string | null
  next_run_at: string | null
  created_at: string
  updated_at: string
}

type SeedItem = {
  torrent_id: string
  site_name: string
  title: string
}

const loading = ref(false)
const error = ref('')
const tasks = ref<ScheduledSeedTask[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)
const statusFilter = ref('')

const formDialogVisible = ref(false)
const editingTask = ref<ScheduledSeedTask | null>(null)

const logsDrawerVisible = ref(false)
const activeTriggerTag = ref('')

// 批量操作相关状态
const tableRef = ref<{ clearSelection: () => void } | null>(null)
const selectedRows = ref<ScheduledSeedTask[]>([])
const batchLoading = ref(false)

const POLL_INTERVAL_MS = 3000
let pollTimer: ReturnType<typeof setInterval> | null = null
let pollRefreshing = false
let fetchSeq = 0

const parseSeeds = (seedsJson: string): SeedItem[] => {
  try {
    const parsed = JSON.parse(seedsJson)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

const parseTargetSites = (json: string): string[] => {
  try {
    const parsed = JSON.parse(json)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

const formatInterval = (minutes: number): string => {
  if (minutes < 60) return `${minutes}分钟`
  if (minutes % 60 === 0) return `${minutes / 60}小时`
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return `${h}小时${m}分钟`
}

const formatStatus = (status: string): string => {
  if (status === 'active') return '运行中'
  if (status === 'paused') return '已暂停'
  if (status === 'completed') return '已完成'
  return status || '未知'
}

const statusTagType = (status: string) => {
  if (status === 'active') return 'success'
  if (status === 'paused') return 'warning'
  if (status === 'completed') return 'info'
  return 'info'
}

const formatDateTime = (raw: string | null): string => {
  if (!raw) return '-'
  const trimmed = raw.trim()
  if (!trimmed) return '-'
  // 服务端已返回本地时间，直接提取时分秒
  const d = new Date(trimmed.replace(' ', 'T'))
  if (isNaN(d.getTime())) return trimmed
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  return `${hh}:${mm}:${ss}`
}

const formatDateTimeFull = (raw: string | null): string => {
  if (!raw) return '-'
  const trimmed = raw.trim()
  if (!trimmed) return '-'
  return trimmed
}

const fetchTasks = async (options: { silent?: boolean } = {}) => {
  const silent = options.silent === true
  const currentFetchSeq = ++fetchSeq
  if (!silent) {
    loading.value = true
    error.value = ''
  }
  try {
    const response = await axios.get('/api/scheduled-seed/tasks', {
      params: {
        page: currentPage.value,
        page_size: pageSize.value,
        status: statusFilter.value,
      },
    })

    if (!response.data?.success) {
      throw new Error(response.data?.message || '获取定时发种任务失败')
    }

    if (currentFetchSeq !== fetchSeq) return
    tasks.value = Array.isArray(response.data.data)
      ? (response.data.data as ScheduledSeedTask[])
      : []
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
          : '获取定时发种任务失败'
      error.value = message
    }
  } finally {
    if (!silent && currentFetchSeq === fetchSeq) {
      loading.value = false
    }
  }
}

const handleStatusChange = () => {
  currentPage.value = 1
  fetchTasks()
}

const handleSizeChange = (size: number) => {
  pageSize.value = size
  currentPage.value = 1
  fetchTasks()
}

const handleCurrentChange = (page: number) => {
  currentPage.value = page
  fetchTasks()
}

const openCreateDialog = () => {
  editingTask.value = null
  formDialogVisible.value = true
}

const openEditDialog = (row: ScheduledSeedTask) => {
  editingTask.value = row
  formDialogVisible.value = true
}

const handleTaskSaved = () => {
  formDialogVisible.value = false
  editingTask.value = null
  fetchTasks()
}

const toggleTask = async (row: ScheduledSeedTask) => {
  try {
    const response = await axios.post(`/api/scheduled-seed/tasks/${row.id}/toggle`)
    if (!response.data?.success) {
      throw new Error(response.data?.message || '操作失败')
    }
    ElMessage.success(response.data?.message || '操作成功')
    await fetchTasks()
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '操作失败'
    ElMessage.error(message)
  }
}

const deleteTask = async (row: ScheduledSeedTask) => {
  try {
    const response = await axios.delete(`/api/scheduled-seed/tasks/${row.id}`)
    if (!response.data?.success) {
      throw new Error(response.data?.message || '删除失败')
    }
    ElMessage.success(response.data?.message || '任务已删除')
    await fetchTasks()
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '删除失败'
    ElMessage.error(message)
  }
}

const triggerTask = async (row: ScheduledSeedTask) => {
  try {
    const response = await axios.post(`/api/scheduled-seed/tasks/${row.id}/trigger`)
    if (!response.data?.success) {
      throw new Error(response.data?.message || '触发失败')
    }
    ElMessage.success(response.data?.message || '已触发发种任务')
    await fetchTasks()
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '触发失败'
    ElMessage.error(message)
  }
}

const handleSelectionChange = (rows: ScheduledSeedTask[]) => {
  selectedRows.value = rows
}

const clearSelection = () => {
  tableRef.value?.clearSelection()
}

const batchSetStatus = async (status: 'active' | 'paused') => {
  const ids = selectedRows.value.map((r) => r.id)
  if (ids.length === 0) return
  batchLoading.value = true
  try {
    const response = await axios.post('/api/scheduled-seed/tasks/batch/status', { ids, status })
    if (!response.data?.success) {
      throw new Error(response.data?.message || '操作失败')
    }
    ElMessage.success(response.data?.message || '操作成功')
    clearSelection()
    await fetchTasks()
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '操作失败'
    ElMessage.error(message)
  } finally {
    batchLoading.value = false
  }
}

const batchDelete = async () => {
  const ids = selectedRows.value.map((r) => r.id)
  if (ids.length === 0) return
  batchLoading.value = true
  try {
    const response = await axios.post('/api/scheduled-seed/tasks/batch/delete', { ids })
    if (!response.data?.success) {
      throw new Error(response.data?.message || '删除失败')
    }
    ElMessage.success(response.data?.message || '批量删除成功')
    clearSelection()
    await fetchTasks()
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '删除失败'
    ElMessage.error(message)
  } finally {
    batchLoading.value = false
  }
}

const batchTrigger = async () => {
  const ids = selectedRows.value.map((r) => r.id)
  if (ids.length === 0) return
  batchLoading.value = true
  try {
    const response = await axios.post('/api/scheduled-seed/tasks/batch/trigger', { ids })
    if (!response.data?.success) {
      throw new Error(response.data?.message || '触发失败')
    }
    ElMessage.success(response.data?.message || '批量触发成功')
    clearSelection()
    await fetchTasks()
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '触发失败'
    ElMessage.error(message)
  } finally {
    batchLoading.value = false
  }
}

const openLogsDrawer = (row: ScheduledSeedTask) => {
  activeTriggerTag.value = row.trigger_tag
  logsDrawerVisible.value = true
}

const handleExpandChange = () => {
  // No-op, handled by el-table internally
}

const runPollRefresh = async () => {
  if (pollRefreshing || loading.value) return
  pollRefreshing = true
  try {
    await fetchTasks({ silent: true })
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
  await fetchTasks()
  emits('ready', fetchTasks)
  startPolling()
})

onBeforeUnmount(() => {
  stopPolling()
})
</script>

<style scoped>
.scheduled-seeding-view {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 0;
  box-sizing: border-box;
}

.toolbar {
  display: flex;
  align-items: center;
  padding: 10px 15px;
  background-color: #ffffff;
  border-bottom: 1px solid #ebeef5;
}

.toolbar-spacer {
  flex: 1;
}

.batch-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-left: 15px;
  padding-left: 15px;
  border-left: 1px solid #ebeef5;
}

.batch-count {
  font-size: 12px;
  color: #909399;
  white-space: nowrap;
}

.batch-count-active {
  color: #409eff;
  font-weight: 600;
}

.pagination-controls {
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

.expand-content {
  padding: 12px 20px;
}

.site-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.site-tag {
  margin: 0;
}

.datetime-cell {
  white-space: pre-line;
  line-height: 1.2;
  font-size: 12px;
}

.action-buttons {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 4px;
  width: 100%;
  height: 100%;
}

.stat-published {
  color: #67c23a;
  font-weight: 600;
}

.stat-separator {
  color: #909399;
  margin: 0 2px;
}

.stat-skipped {
  color: #e6a23c;
  font-weight: 600;
}
</style>
