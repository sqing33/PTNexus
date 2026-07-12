<template>
  <el-drawer
    :model-value="visible"
    title="发布日志"
    size="720px"
    direction="rtl"
    @update:model-value="$emit('update:visible', $event)"
    @open="handleOpen"
    @close="handleClose"
  >
    <div class="logs-drawer-body">
      <div class="logs-toolbar" v-if="total > 0">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
          background
          small
        />
      </div>

      <el-table
        :data="rows"
        v-loading="loading"
        border
        style="width: 100%"
        empty-text="暂无发布日志"
        class="logs-table"
      >
        <el-table-column label="时间" width="150" align="center">
          <template #default="{ row }">
            <div class="datetime-cell">{{ formatDateTime(row.created_at) }}</div>
          </template>
        </el-table-column>

        <el-table-column label="种子标题" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="title-cell">
              <div class="main-title" :title="row.title">{{ row.title || '-' }}</div>
              <div class="subtitle" v-if="row.subtitle" :title="row.subtitle">
                {{ row.subtitle }}
              </div>
            </div>
          </template>
        </el-table-column>

        <el-table-column prop="source_site" label="源站" width="80" align="center" />

        <el-table-column prop="target_site" label="目标站" width="80" align="center" />

        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status)" size="small">
              {{ formatStatus(row.status) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="失败原因" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.status === 'failed' && row.logs" class="fail-reason">{{ row.logs }}</span>
            <el-button
              v-else-if="row.logs"
              size="small"
              type="primary"
              link
              @click="showLogDetail(row)"
            >
              查看
            </el-button>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog
      v-model="logDetailVisible"
      title="日志详情"
      width="600px"
      append-to-body
    >
      <pre class="log-detail-content">{{ logDetailContent }}</pre>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import axios from 'axios'

type PublishLogRow = {
  id: number | string
  created_at: string
  title?: string | null
  subtitle?: string | null
  source_site: string
  target_site: string
  status: string
  logs?: string | null
  [key: string]: unknown
}

const props = defineProps<{
  visible: boolean
  triggerTag: string
}>()

defineEmits<{
  'update:visible': [value: boolean]
}>()

const loading = ref(false)
const rows = ref<PublishLogRow[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(20)

const logDetailVisible = ref(false)
const logDetailContent = ref('')

const POLL_INTERVAL_MS = 3000
let pollTimer: ReturnType<typeof setInterval> | null = null
let pollRefreshing = false
let fetchSeq = 0

const statusTagType = (status: string) => {
  if (status === 'queued') return 'info'
  if (status === 'success') return 'success'
  if (status === 'edited') return 'success'
  if (status === 'exists') return 'warning'
  if (status === 'filtered') return 'warning'
  if (status === 'failed') return 'danger'
  if (status === 'cancelled') return 'info'
  if (status === 'pre_check_limit') return 'danger'
  return 'info'
}

const formatStatus = (status: string) => {
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

const formatDateTime = (raw: string) => {
  const trimmed = (raw || '').trim()
  if (!trimmed) return '-'
  const d = new Date(trimmed)
  if (isNaN(d.getTime())) return trimmed
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

const showLogDetail = (row: PublishLogRow) => {
  logDetailContent.value = row.logs || '无日志内容'
  logDetailVisible.value = true
}

const fetchLogs = async (options: { silent?: boolean } = {}) => {
  const silent = options.silent === true
  const currentFetchSeq = ++fetchSeq
  if (!silent) {
    loading.value = true
  }
  try {
    const response = await axios.get('/api/publish_logs', {
      params: {
        page: currentPage.value,
        page_size: pageSize.value,
        scene: 'scheduled_seeding',
        trigger: props.triggerTag,
      },
    })

    if (!response.data?.success) {
      throw new Error(response.data?.message || '获取发布日志失败')
    }

    if (currentFetchSeq !== fetchSeq) return
    rows.value = Array.isArray(response.data.data)
      ? (response.data.data as PublishLogRow[])
      : []
    total.value = Number(response.data.total || 0)
  } catch (e) {
    if (currentFetchSeq !== fetchSeq) return
    if (!silent) {
      console.error('获取发布日志失败:', e)
    }
  } finally {
    if (!silent && currentFetchSeq === fetchSeq) {
      loading.value = false
    }
  }
}

const handleSizeChange = (size: number) => {
  pageSize.value = size
  currentPage.value = 1
  fetchLogs()
}

const handleCurrentChange = (page: number) => {
  currentPage.value = page
  fetchLogs()
}

const handleOpen = () => {
  currentPage.value = 1
  fetchLogs()
  startPolling()
}

const handleClose = () => {
  stopPolling()
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

watch(
  () => props.visible,
  (val) => {
    if (!val) {
      stopPolling()
    }
  },
)

onBeforeUnmount(() => {
  stopPolling()
})
</script>

<style scoped>
.logs-drawer-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 0 4px;
}

.logs-toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.logs-table {
  flex: 1;
}

.datetime-cell {
  white-space: pre-line;
  line-height: 1.2;
  font-size: 12px;
}

.title-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.main-title {
  font-size: 13px;
  line-height: 1.25;
}

.subtitle {
  font-size: 12px;
  color: #909399;
  line-height: 1.2;
}

.text-muted {
  color: #c0c4cc;
}

.fail-reason {
  color: #f56c6c;
  font-size: 12px;
  line-height: 1.4;
}

.log-detail-content {
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 13px;
  line-height: 1.5;
  max-height: 400px;
  overflow-y: auto;
  background-color: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
  margin: 0;
}
</style>
