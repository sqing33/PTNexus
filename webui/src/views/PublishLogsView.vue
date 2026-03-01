<template>
  <div class="publish-logs-view">
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      center
      style="margin-bottom: 15px"
    />

    <div class="search-and-controls glass-table">
      <el-input
        v-model="searchQuery"
        placeholder="搜索标题/副标题/种子ID/站点..."
        clearable
        class="search-input"
        style="width: 320px; margin-right: 12px"
        @keyup.enter="applyFilters"
      />

      <el-select
        v-model="sceneFilter"
        placeholder="场景"
        clearable
        style="width: 140px; margin-right: 12px"
      >
        <el-option label="一种多站" value="multi_site" />
        <el-option label="一站多种" value="multi_torrent" />
      </el-select>

      <el-select
        v-model="triggerFilter"
        placeholder="触发方式"
        clearable
        style="width: 140px; margin-right: 12px"
      >
        <el-option label="手动" value="manual" />
        <el-option label="队列" value="queue" />
      </el-select>

      <el-select
        v-model="statusFilter"
        placeholder="状态"
        clearable
        style="width: 150px; margin-right: 12px"
      >
        <el-option label="成功" value="success" />
        <el-option label="失败" value="failed" />
        <el-option label="预检查限制" value="pre_check_limit" />
      </el-select>

      <el-input
        v-model="targetSiteFilter"
        placeholder="目标站点"
        clearable
        style="width: 140px; margin-right: 12px"
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
        height="100%"
        empty-text="暂无发种日志"
        class="glass-table"
      >
        <el-table-column prop="created_at" label="时间" width="160" align="center" />
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
              <div class="subtitle-line" :title="scope.row.subtitle">{{ scope.row.subtitle || '' }}</div>
              <div class="main-title-line" :title="scope.row.title">{{ scope.row.title || '' }}</div>
            </div>
          </template>
        </el-table-column>

        <el-table-column label="状态" width="120" align="center">
          <template #default="scope">
            <el-tag :type="statusTagType(scope.row.status)" effect="dark">
              {{ formatStatus(scope.row.status) }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="210" align="center" fixed="right">
          <template #default="scope">
            <el-button size="small" type="primary" @click="openLogs(scope.row)">日志</el-button>
            <el-button
              size="small"
              type="info"
              @click="openAutoAdd(scope.row)"
              :disabled="!scope.row.auto_add_result"
            >
              下载器
            </el-button>
            <a
              v-if="scope.row.result_url"
              :href="scope.row.result_url"
              target="_blank"
              rel="noopener noreferrer"
              style="text-decoration: none"
            >
              <el-button size="small" type="success">打开</el-button>
            </a>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="80%" top="8vh">
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

const statusTagType = (status: string) => {
  if (status === 'success') return 'success'
  if (status === 'pre_check_limit') return 'warning'
  if (status === 'failed') return 'danger'
  return 'info'
}

const formatStatus = (status: string) => {
  if (status === 'success') return '成功'
  if (status === 'pre_check_limit') return '预检查限制'
  if (status === 'failed') return '失败'
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
  dialogContent.value = row.logs || ''
  dialogVisible.value = true
}

const openAutoAdd = (row: any) => {
  dialogTitle.value = `下载器结果 - ${row.target_site || ''}`
  const raw = row.auto_add_result || ''
  try {
    dialogContent.value = JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    dialogContent.value = raw
  }
  dialogVisible.value = true
}

onMounted(async () => {
  await fetchLogs()
  emits('ready', fetchLogs)
})
</script>

<style scoped>
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
</style>

