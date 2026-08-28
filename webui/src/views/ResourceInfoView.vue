<template>
  <div class="resource-info-view">
    <div class="search-and-controls glass-table">
      <el-input
        v-model="searchQuery"
        placeholder="搜索标题/国家/豆瓣ID/IMDbID/TMDbID..."
        clearable
        class="search-input"
        style="width: 320px; margin-right: 15px"
        @keyup.enter="applyFilters"
      />
      <el-button type="primary" plain @click="applyFilters">查询</el-button>
      <el-button style="margin-left: 8px" @click="clearFilters">清空</el-button>
      <el-button type="primary" plain style="margin-left: 8px" @click="fetchList">刷新</el-button>
    </div>

    <div class="table-container glass-table">
      <el-table :data="items" v-loading="loading" style="width: 100%" size="small">
        <el-table-column label="海报" width="72">
          <template #default="{ row }">
            <el-image
              v-if="row.poster_url"
              :src="row.poster_url"
              :preview-src-list="[row.poster_url]"
              preview-teleported
              fit="cover"
              class="poster-thumb"
            />
            <span v-else class="empty-text">N/A</span>
          </template>
        </el-table-column>
        <el-table-column label="标题" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span class="cell-text">{{ row.title || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制标题" @click="copyText(row.title, '标题')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="年份" width="76">
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.year }">{{ row.year || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制年份" @click="copyText(row.year, '年份')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="国家" width="110" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.country }">{{ row.country || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制国家" @click="copyText(row.country, '国家')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="豆瓣ID" width="120">
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.douban_id }">{{ row.douban_id || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制豆瓣ID" @click="copyText(row.douban_id, '豆瓣ID')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="IMDbID" width="130">
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.imdb_id }">{{ row.imdb_id || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制IMDbID" @click="copyText(row.imdb_id, 'IMDbID')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="TMDbID" width="120">
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.tmdb_id }">{{ row.tmdb_id || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制TMDbID" @click="copyText(row.tmdb_id, 'TMDbID')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="简介" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.summary }">{{ row.summary || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制简介" @click="copyText(row.summary, '简介')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="160">
          <template #default="{ row }">
            <div class="cell-with-copy">
              <span :class="{ 'empty-text': !row.updated_at }">{{ row.updated_at || 'N/A' }}</span>
              <el-button text size="small" :icon="CopyDocument" title="复制更新时间" @click="copyText(row.updated_at, '更新时间')" />
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link :icon="View" @click="openDetail(row)">查看</el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-container">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @current-change="fetchList"
          @size-change="handleSizeChange"
        />
      </div>
    </div>

    <el-dialog
      v-model="detailVisible"
      title="资源信息详情"
      width="560px"
      append-to-body
      class="resource-detail-dialog"
    >
      <div v-if="selectedItem" class="detail-body">
        <div class="detail-toolbar">
          <el-button type="primary" size="small" :icon="CopyDocument" @click="copyAll(selectedItem)">复制全部</el-button>
        </div>
        <div class="detail-poster">
          <el-image
            v-if="selectedItem.poster_url"
            :src="selectedItem.poster_url"
            :preview-src-list="[selectedItem.poster_url]"
            preview-teleported
            fit="cover"
            class="detail-poster-img"
          />
          <span v-else class="empty-text">无海报</span>
        </div>

        <div class="detail-fields">
          <div class="detail-field">
            <span class="detail-label">标题</span>
            <span class="detail-value">{{ selectedItem.title || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.title, '标题')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">年份</span>
            <span class="detail-value">{{ selectedItem.year || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.year, '年份')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">国家</span>
            <span class="detail-value">{{ selectedItem.country || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.country, '国家')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">豆瓣ID</span>
            <span class="detail-value">{{ selectedItem.douban_id || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.douban_id, '豆瓣ID')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">IMDbID</span>
            <span class="detail-value">{{ selectedItem.imdb_id || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.imdb_id, 'IMDbID')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">TMDbID</span>
            <span class="detail-value">{{ selectedItem.tmdb_id || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.tmdb_id, 'TMDbID')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">海报地址</span>
            <span class="detail-value break-all">{{ selectedItem.poster_url || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.poster_url, '海报地址')" />
            <el-button text :icon="Link" title="跳转打开" @click="openPoster(selectedItem.poster_url)" />
          </div>
          <div class="detail-field detail-field-block">
            <span class="detail-label">简介</span>
            <span class="detail-value detail-summary">{{ selectedItem.summary || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.summary, '简介')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">创建时间</span>
            <span class="detail-value">{{ selectedItem.created_at || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.created_at, '创建时间')" />
          </div>
          <div class="detail-field">
            <span class="detail-label">更新时间</span>
            <span class="detail-value">{{ selectedItem.updated_at || 'N/A' }}</span>
            <el-button text :icon="CopyDocument" title="复制" @click="copyText(selectedItem.updated_at, '更新时间')" />
          </div>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'
import { CopyDocument, View, Link } from '@element-plus/icons-vue'

interface ResourceInfoItem {
  id: number
  title: string
  year: string
  country: string
  douban_id: string
  imdb_id: string
  tmdb_id: string
  poster_url: string
  summary: string
  created_at: string
  updated_at: string
}

const items = ref<ResourceInfoItem[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const searchQuery = ref('')
const loading = ref(false)

const detailVisible = ref(false)
const selectedItem = ref<ResourceInfoItem | null>(null)

async function copyText(text: string, label: string) {
  if (!text) {
    ElMessage.warning('内容为空，无法复制')
    return
  }
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
    } else {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
    }
    ElMessage.success(`${label} 已复制`)
  } catch {
    ElMessage.error('复制失败')
  }
}

function openDetail(row: ResourceInfoItem) {
  selectedItem.value = row
  detailVisible.value = true
}

function copyAll(item: ResourceInfoItem) {
  const lines = [
    `标题: ${item.title || 'N/A'}`,
    `年份: ${item.year || 'N/A'}`,
    `国家: ${item.country || 'N/A'}`,
    `豆瓣ID: ${item.douban_id || 'N/A'}`,
    `IMDbID: ${item.imdb_id || 'N/A'}`,
    `TMDbID: ${item.tmdb_id || 'N/A'}`,
    `海报地址: ${item.poster_url || 'N/A'}`,
    `简介: ${item.summary || 'N/A'}`,
    `创建时间: ${item.created_at || 'N/A'}`,
    `更新时间: ${item.updated_at || 'N/A'}`,
  ]
  copyText(lines.join('\n'), '全部信息')
}

function openPoster(url: string) {
  if (!url) {
    ElMessage.warning('海报地址为空，无法打开')
    return
  }
  window.open(url, '_blank', 'noopener')
}

async function fetchList() {
  loading.value = true
  try {
    const params = new URLSearchParams()
    params.set('page', String(page.value))
    params.set('page_size', String(pageSize.value))
    if (searchQuery.value.trim()) {
      params.set('keyword', searchQuery.value.trim())
    }
    const response = await axios.get(`/api/resource_info?${params.toString()}`)
    items.value = response.data.items || []
    total.value = response.data.total || 0
  } catch (error) {
    const message = axios.isAxiosError(error) ? error.message : String(error)
    ElMessage.error('获取资源信息失败: ' + message)
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 1
  fetchList()
}

function clearFilters() {
  searchQuery.value = ''
  page.value = 1
  fetchList()
}

function handleSizeChange() {
  page.value = 1
  fetchList()
}

onMounted(fetchList)
</script>

<style scoped>
.resource-info-view {
  padding: 15px;
  display: flex;
  flex-direction: column;
  gap: 15px;
  height: 100%;
  overflow: auto;
}

.search-and-controls {
  padding: 15px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
}

.table-container {
  padding: 15px;
  border-radius: 8px;
}

.poster-thumb {
  width: 48px;
  height: 64px;
  border-radius: 4px;
  display: block;
}

.cell-with-copy {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}

.cell-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.empty-text {
  color: var(--el-text-color-placeholder);
}

.pagination-container {
  display: flex;
  justify-content: flex-end;
  margin-top: 15px;
}

.detail-body {
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.detail-toolbar {
  position: absolute;
  top: 16px;
  right: 20px;
}

.detail-poster {
  flex-shrink: 0;
  width: 120px;
}

.detail-poster-img {
  width: 120px;
  height: 160px;
  border-radius: 6px;
  display: block;
}

.detail-fields {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.detail-field {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.detail-field-block {
  align-items: flex-start;
}

.detail-label {
  width: 64px;
  flex-shrink: 0;
  color: var(--el-text-color-secondary);
  text-align: right;
}

.detail-value {
  flex: 1;
  min-width: 0;
  word-break: break-word;
}

.detail-summary {
  white-space: pre-wrap;
  line-height: 1.6;
}

.break-all {
  word-break: break-all;
}
</style>
