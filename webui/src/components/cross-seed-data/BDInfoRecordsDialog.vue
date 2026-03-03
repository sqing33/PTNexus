<template>
  <!-- 处理记录查看弹窗 -->
  <div v-if="visible" class="modal-overlay">
    <el-card class="record-view-card" shadow="always">
      <div class="record-view-content">
        <!-- 自定义标签导航 -->
        <div class="record-tabs-header">
          <div class="record-tabs-nav">
            <div class="tab-item active">BDInfo获取记录</div>
          </div>
          <div class="record-close-btn">
            <el-button type="danger" circle @click="close" plain>X</el-button>
          </div>
        </div>

        <!-- 隐藏默认头部的标签页 -->
        <el-tabs model-value="bdinfo" type="border-card" class="record-tabs" :show-header="false">
          <!-- BDInfo获取记录标签页 -->
          <el-tab-pane label="BDInfo获取记录" name="bdinfo">
            <template #label>
              <span>BDInfo获取记录</span>
            </template>
            <div class="tab-header">
              <div class="bdinfo-filter-controls">
                <!-- BDInfo状态筛选 -->
                <el-radio-group
                  v-model="bdinfoStatusFilter"
                  @change="handleBDInfoStatusChange"
                  size="small"
                >
                  <el-radio-button label="">全部</el-radio-button>
                  <el-radio-button label="processing">获取中</el-radio-button>
                  <el-radio-button label="completed">已完成</el-radio-button>
                  <el-radio-button label="failed">失败</el-radio-button>
                </el-radio-group>
              </div>
              <div class="tab-controls">
                <!-- 强制自动刷新状态显示 -->
                <el-button type="success" size="small" disabled> 自动刷新中 </el-button>
              </div>
            </div>

            <!-- BDInfo记录表格 -->
            <div class="bdinfo-records-table-container" v-if="bdinfoRecords.length > 0">
              <el-table :data="bdinfoRecords" style="width: 100%" size="small" stripe>
                <el-table-column prop="title" label="种子标题" show-overflow-tooltip />
                <el-table-column prop="nickname" label="站点" width="100" align="center">
                  <template #default="scope">
                    <div class="mapped-cell">{{ scope.row.nickname }}</div>
                  </template>
                </el-table-column>
                <el-table-column prop="seed_id" label="种子ID" width="60" align="center">
                  <template #default="scope">
                    <span>{{ scope.row.seed_id.split('_')[1] }}</span>
                  </template>
                </el-table-column>
                <el-table-column prop="mediainfo_status" label="状态" width="80" align="center">
                  <template #default="scope">
                    <el-tag :type="getBDInfoStatusType(scope.row.mediainfo_status)" size="small">
                      {{ getBDInfoStatusText(scope.row.mediainfo_status) }}
                    </el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="bdinfo_started_at" label="开始时间" width="140" align="center">
                  <template #default="scope">
                    <span v-if="scope.row.bdinfo_started_at" class="datetime-cell">
                      {{ formatDateTime(scope.row.bdinfo_started_at) }}
                    </span>
                    <span v-else>-</span>
                  </template>
                </el-table-column>
                <el-table-column prop="duration" label="耗时" width="80" align="center">
                  <template #default="scope">
                    <span
                      v-if="
                        scope.row.mediainfo_status === 'processing_bdinfo' && scope.row.progress_info
                      "
                    >
                      {{ scope.row.progress_info.elapsed_time }}
                    </span>
                    <span v-else>{{ calculateDuration(scope.row) }}</span>
                  </template>
                </el-table-column>
                <el-table-column label="剩余时间" width="100" align="center">
                  <template #default="scope">
                    <span
                      v-if="
                        scope.row.mediainfo_status === 'processing_bdinfo' &&
                        scope.row.progress_info &&
                        scope.row.progress_info.remaining_time
                      "
                    >
                      {{ scope.row.progress_info.remaining_time }}
                    </span>
                    <span v-else>-</span>
                  </template>
                </el-table-column>
                <el-table-column label="进度" width="100" align="center">
                  <template #default="scope">
                    <div
                      v-if="
                        scope.row.mediainfo_status === 'processing_bdinfo' && scope.row.progress_info
                      "
                      style="text-align: center"
                    >
                      <el-progress
                        :percentage="scope.row.progress_info?.progress_percent || 0"
                        :status="
                          (scope.row.progress_info?.progress_percent || 0) === 100 ? 'success' : ''
                        "
                        :stroke-width="6"
                        :show-text="false"
                      />
                      <div style="font-size: 12px; margin-top: 4px; color: #606266">
                        {{ scope.row.progress_info?.progress_percent || 0 }}%
                      </div>
                    </div>
                    <div
                      v-else-if="scope.row.mediainfo_status === 'completed'"
                      style="text-align: center"
                    >
                      <el-progress
                        :percentage="100"
                        status="success"
                        :stroke-width="6"
                        :show-text="false"
                      />
                      <div style="font-size: 12px; margin-top: 4px; color: #606266">100%</div>
                    </div>
                    <span v-else>-</span>
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="80" align="center">
                  <template #default="scope">
                    <el-button size="small" type="primary" @click="viewBDInfoDetails(scope.row)">
                      详情
                    </el-button>
                    <el-button
                      v-if="shouldShowRetryButton(scope.row)"
                      size="small"
                      type="warning"
                      @click="retryBDInfo(scope.row)"
                      style="margin-left: 0"
                      :loading="retryingSeeds.has(scope.row.seed_id)"
                    >
                      重试
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>

            <!-- 无BDInfo记录时的显示 -->
            <div v-if="bdinfoRecords.length === 0 && !bdinfoRecordsLoading" class="no-records">
              <el-empty description="暂无BDInfo获取记录" />
            </div>
          </el-tab-pane>
        </el-tabs>
      </div>
    </el-card>
  </div>

  <!-- BDInfo详情查看弹窗 -->
  <div v-if="bdinfoDetailDialogVisible" class="modal-overlay">
    <el-card class="bdinfo-detail-card" shadow="always">
      <template #header>
        <div class="modal-header">
          <span>BDInfo详情 - {{ selectedBDInfoRecord?.title }}</span>
          <el-button type="danger" circle @click="closeBDInfoDetailDialog" plain>X</el-button>
        </div>
      </template>
      <div class="bdinfo-detail-content">
        <el-descriptions :column="2" border>
          <el-descriptions-item label="种子标题">
            {{ selectedBDInfoRecord?.title }}
          </el-descriptions-item>
          <el-descriptions-item label="站点">
            {{ selectedBDInfoRecord?.nickname }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag
              :type="getBDInfoStatusType(selectedBDInfoRecord?.mediainfo_status)"
              size="small"
            >
              {{ getBDInfoStatusText(selectedBDInfoRecord?.mediainfo_status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="任务ID">
            <div v-if="selectedBDInfoRecord?.bdinfo_task_id" class="task-id-cell">
              <span>{{ selectedBDInfoRecord.bdinfo_task_id }}</span>
              <el-button
                type="text"
                size="small"
                @click="copyToClipboard(selectedBDInfoRecord.bdinfo_task_id)"
                style="margin-left: 5px; padding: 0"
              >
                <el-icon><CopyDocument /></el-icon>
              </el-button>
            </div>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item label="开始时间">
            <span class="datetime-cell">
              {{
                selectedBDInfoRecord?.bdinfo_started_at
                  ? formatDateTime(selectedBDInfoRecord.bdinfo_started_at)
                  : '-'
              }}
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="完成时间">
            <span class="datetime-cell">
              {{
                selectedBDInfoRecord?.bdinfo_completed_at
                  ? formatDateTime(selectedBDInfoRecord.bdinfo_completed_at)
                  : '-'
              }}
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="耗时">
            {{ calculateDuration(selectedBDInfoRecord) }}
          </el-descriptions-item>
          <el-descriptions-item label="是否为BDInfo">
            <el-tag :type="selectedBDInfoRecord?.is_bdinfo ? 'success' : 'info'" size="small">
              {{ selectedBDInfoRecord?.is_bdinfo ? '是' : '否' }}
            </el-tag>
          </el-descriptions-item>
        </el-descriptions>

        <!-- 错误信息 -->
        <div v-if="selectedBDInfoRecord?.bdinfo_error" class="error-section">
          <h4 style="margin: 15px 0 10px 0; color: #f56c6c">错误信息</h4>
          <el-alert
            :title="selectedBDInfoRecord.bdinfo_error"
            type="error"
            :closable="false"
            show-icon
          />
        </div>

        <!-- MediaInfo/BDInfo内容 -->
        <div v-if="selectedBDInfoRecord?.mediainfo" class="mediainfo-section">
          <h4 style="margin: 15px 0 10px 0; color: #606266">
            {{ selectedBDInfoRecord?.is_bdinfo ? 'BDInfo' : 'MediaInfo' }} 内容
          </h4>
          <el-input
            type="textarea"
            :model-value="selectedBDInfoRecord.mediainfo"
            :rows="15"
            class="code-font"
            readonly
          />
          <div style="margin-top: 10px; text-align: right">
            <el-button
              type="primary"
              size="small"
              @click="copyToClipboard(selectedBDInfoRecord.mediainfo)"
            >
              复制内容
            </el-button>
          </div>
        </div>
      </div>
      <div class="bdinfo-detail-footer">
        <el-button @click="closeBDInfoDetailDialog">关闭</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import axios from 'axios'

const visible = defineModel<boolean>({ default: false })
const emit = defineEmits<{
  (e: 'closed'): void
}>()

interface BDInfoRecord {
  seed_id: string
  title: string
  site_name: string
  nickname?: string
  mediainfo_status: string
  bdinfo_task_id?: string
  bdinfo_started_at?: string
  bdinfo_completed_at?: string
  bdinfo_error?: string
  mediainfo?: string
  is_bdinfo: boolean
  progress_info?: {
    progress_percent?: number
    elapsed_time?: string
    remaining_time?: string
    last_progress_update?: string
  }
}

const bdinfoRecords = ref<BDInfoRecord[]>([])
const bdinfoRecordsLoading = ref(false)
const bdinfoStatusFilter = ref('') // BDInfo状态筛选
const bdinfoDetailDialogVisible = ref(false)
const selectedBDInfoRecord = ref<BDInfoRecord | null>(null)
const retryingSeeds = ref<Set<string>>(new Set()) // 正在重试的种子ID集合

const bdinfoRefreshTimer = ref<ReturnType<typeof setInterval> | null>(null)
const BDINFO_REFRESH_INTERVAL = 5000 // 5秒刷新一次

const close = () => {
  visible.value = false
}

watch(visible, async (isOpen, wasOpen) => {
  if (isOpen && !wasOpen) {
    await refreshBDInfoRecords()
    startBDInfoAutoRefresh()
    return
  }

  if (!isOpen && wasOpen) {
    stopBDInfoAutoRefresh()
    bdinfoDetailDialogVisible.value = false
    selectedBDInfoRecord.value = null
    emit('closed')
  }
})

const handleBDInfoStatusChange = async (value: string) => {
  bdinfoStatusFilter.value = value
  await refreshBDInfoRecords()
}

const refreshBDInfoRecords = async () => {
  bdinfoRecordsLoading.value = true
  try {
    const params = new URLSearchParams({
      status_filter: bdinfoStatusFilter.value,
    })

    const existingProgressInfo = new Map<string, NonNullable<BDInfoRecord['progress_info']>>()
    for (const record of bdinfoRecords.value) {
      if (record.mediainfo_status === 'processing_bdinfo' && record.progress_info) {
        existingProgressInfo.set(record.seed_id, record.progress_info)
      }
    }

    const response = await axios.get(`/api/migrate/bdinfo_records?${params.toString()}`)
    const result = response.data

    if (result.success) {
      const newRecords: BDInfoRecord[] = result.data || []
      for (const newRecord of newRecords) {
        if (
          newRecord.mediainfo_status === 'processing_bdinfo' &&
          existingProgressInfo.has(newRecord.seed_id)
        ) {
          newRecord.progress_info = existingProgressInfo.get(newRecord.seed_id)
        }
      }
      bdinfoRecords.value = newRecords

      const progressPromises: Array<Promise<void>> = []
      for (const record of bdinfoRecords.value) {
        if (record.mediainfo_status === 'processing_bdinfo' && record.bdinfo_task_id) {
          progressPromises.push(
            (async () => {
              try {
                const progressResponse = await axios.get(`/api/migrate/bdinfo_status/${record.seed_id}`)
                const progressResult = progressResponse.data
                if (progressResult.task_status && progressResult.progress_info) {
                  record.progress_info = progressResult.progress_info
                }
              } catch (error) {
                console.error(`获取BDInfo进度失败: ${record.seed_id}`, error)
              }
            })(),
          )
        }
      }

      await Promise.all(progressPromises)
      return
    }

    ElMessage.error(result.message || '获取BDInfo记录失败')
  } catch (error: unknown) {
    console.error('获取BDInfo记录时出错:', error)
    const message = error instanceof Error ? error.message : '网络错误'
    ElMessage.error(message)
  } finally {
    bdinfoRecordsLoading.value = false
  }
}

const startBDInfoAutoRefresh = () => {
  stopBDInfoAutoRefresh()
  bdinfoRefreshTimer.value = setInterval(async () => {
    if (!visible.value) {
      stopBDInfoAutoRefresh()
      return
    }
    await refreshBDInfoRecords()
  }, BDINFO_REFRESH_INTERVAL)
}

const stopBDInfoAutoRefresh = () => {
  if (bdinfoRefreshTimer.value) {
    clearInterval(bdinfoRefreshTimer.value)
    bdinfoRefreshTimer.value = null
  }
}

const getBDInfoStatusType = (status?: string) => {
  switch (status) {
    case 'queued':
      return 'info'
    case 'processing_bdinfo':
    case 'processing':
      return 'warning'
    case 'completed':
      return 'success'
    case 'failed':
      return 'danger'
    default:
      return 'info'
  }
}

const getBDInfoStatusText = (status?: string) => {
  switch (status) {
    case 'queued':
      return '等待中'
    case 'processing_bdinfo':
    case 'processing':
      return '获取中'
    case 'completed':
      return '已完成'
    case 'failed':
      return '失败'
    default:
      return '未知'
  }
}

const formatDateTime = (dateString: string) => {
  if (!dateString) return ''

  try {
    const date = new Date(dateString)
    if (isNaN(date.getTime())) return dateString

    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    const seconds = String(date.getSeconds()).padStart(2, '0')
    return `${year}-${month}-${day}\n${hours}:${minutes}:${seconds}`
  } catch {
    return dateString
  }
}

const calculateDuration = (record: BDInfoRecord | null | undefined) => {
  if (!record?.bdinfo_started_at) return '-'

  const start = new Date(record.bdinfo_started_at)
  const end = record.bdinfo_completed_at ? new Date(record.bdinfo_completed_at) : new Date()
  const diff = end.getTime() - start.getTime()

  if (diff < 0) return '-'
  if (diff < 60000) return `${Math.floor(diff / 1000)}秒`
  if (diff < 3600000) return `${Math.floor(diff / 60000)}分钟`
  return `${Math.floor(diff / 3600000)}小时`
}

const copyToClipboard = async (text?: string) => {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败')
  }
}

const viewBDInfoDetails = (record: BDInfoRecord) => {
  selectedBDInfoRecord.value = record || null
  bdinfoDetailDialogVisible.value = true
}

const closeBDInfoDetailDialog = () => {
  bdinfoDetailDialogVisible.value = false
  selectedBDInfoRecord.value = null
}

const isTaskStuck = (record: BDInfoRecord) => {
  if (!record.bdinfo_started_at) return true

  const startTime = new Date(record.bdinfo_started_at)
  const now = new Date()
  const runningMinutes = (now.getTime() - startTime.getTime()) / (1000 * 60)

  if (runningMinutes > 30) {
    if (!record.progress_info || record.progress_info.progress_percent === 0) {
      return true
    }

    if (record.progress_info.last_progress_update) {
      const lastUpdateTime = new Date(record.progress_info.last_progress_update)
      const stagnantMinutes = (now.getTime() - lastUpdateTime.getTime()) / (1000 * 60)

      if ((record.progress_info.progress_percent || 0) < 10) {
        return stagnantMinutes > 15
      }
      if ((record.progress_info.progress_percent || 0) < 50) {
        return stagnantMinutes > 10
      }
      return stagnantMinutes > 5
    }
  }

  return false
}

const shouldShowRetryButton = (record: BDInfoRecord) => {
  if (record.mediainfo_status === 'failed') {
    return true
  }
  if (record.mediainfo_status === 'processing_bdinfo') {
    return isTaskStuck(record)
  }
  return false
}

const retryBDInfo = async (record: BDInfoRecord) => {
  try {
    retryingSeeds.value.add(record.seed_id)

    try {
      await axios.post('/api/migrate/cleanup_bdinfo_process', {
        seed_id: record.seed_id,
      })
    } catch (error) {
      console.warn('清理进程失败:', error)
    }

    const response = await axios.post('/api/migrate/restart_bdinfo', {
      seed_id: record.seed_id,
    })
    const result = response.data

    if (result.success) {
      ElMessage.success('BDInfo重新获取任务已启动')
      await refreshBDInfoRecords()
      startBDInfoAutoRefresh()
      return
    }

    ElMessage.error(result.message || '启动BDInfo重新获取失败')
  } catch (error: unknown) {
    const message = error instanceof Error ? error.message : '网络错误'
    ElMessage.error(message)
  } finally {
    retryingSeeds.value.delete(record.seed_id)
  }
}

onUnmounted(() => {
  stopBDInfoAutoRefresh()
})
</script>

<style scoped lang="scss">
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 2000;
}

.record-view-card {
  width: 1200px;
  height: 800px;
  max-height: 800px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

:deep(.record-view-card .el-card__body) {
  padding: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-height: 0;
}

.record-view-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  overflow-x: hidden;
}

.no-records {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.mapped-cell {
  text-align: center;
  line-height: 1.4;
}

.datetime-cell {
  white-space: pre-line;
  line-height: 1.2;
}

.record-tabs {
  flex: 1;
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.record-tabs :deep(.el-tabs__content) {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 15px;
  overflow: hidden;
}

.record-tabs :deep(.el-tab-pane) {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.bdinfo-records-table-container {
  flex: 1;
  overflow-y: auto;
  width: 100%;
  margin-top: 10px;

  &::-webkit-scrollbar {
    width: 8px;
  }
  &::-webkit-scrollbar-track {
    background: #f1f1f1;
    border-radius: 4px;
  }
  &::-webkit-scrollbar-thumb {
    background: #c1c1c1;
    border-radius: 4px;
  }
  &::-webkit-scrollbar-thumb:hover {
    background: #a8a8a8;
  }
}

.bdinfo-filter-controls {
  margin-bottom: 0;
}

.tab-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 0;
}

.tab-controls {
  display: flex;
  gap: 10px;
}

.record-tabs-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #dcdfe6;
}

.record-tabs-nav {
  display: flex;
  gap: 20px;
}

.tab-item {
  padding: 8px 16px;
  cursor: pointer;
  border-bottom: 2px solid transparent;
  color: #606266;
  font-weight: 500;
  transition: all 0.3s ease;
}

.tab-item:hover {
  color: #409eff;
}

.tab-item.active {
  color: #409eff;
  border-bottom-color: #409eff;
}

.record-close-btn {
  flex-shrink: 0;
}

.record-tabs :deep(.el-tabs__header) {
  display: none;
}

.bdinfo-detail-card {
  width: 900px;
  max-width: 95vw;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

:deep(.bdinfo-detail-card .el-card__body) {
  flex: 1;
  overflow: auto;
}

.bdinfo-detail-content {
  flex: 1;
  overflow: auto;
}

.bdinfo-detail-footer {
  padding-top: 10px;
  display: flex;
  justify-content: flex-end;
}

.task-id-cell {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

@media (max-width: 768px) {
  .record-view-card {
    width: calc(100vw - 16px);
    max-width: calc(100vw - 16px);
    height: calc(100vh - 20px);
    max-height: calc(100vh - 20px);
  }

  .tab-header {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }

  .tab-controls {
    flex-wrap: wrap;
  }

  .record-tabs-nav {
    gap: 8px;
    overflow-x: auto;
    padding-bottom: 4px;
  }

  .tab-item {
    padding: 8px 10px;
    white-space: nowrap;
  }
}
</style>
