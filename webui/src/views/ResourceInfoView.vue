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
            :src="cleanPoster(row.poster_url)"
            :preview-src-list="[cleanPoster(row.poster_url)]"
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
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link :icon="View" @click="openDetail(row)">查看</el-button>
            <el-button type="primary" link :icon="Edit" @click="openEdit(row)">编辑</el-button>
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
      width="85%"
      :style="{ height: '85%' }"
      append-to-body
      class="resource-detail-dialog"
    >
      <div v-if="selectedItem" class="detail-body">
        <div class="detail-toolbar">
          <el-button type="primary" size="small" :icon="CopyDocument" @click="copyAll(selectedItem)">复制全部</el-button>
          <el-button size="small" :icon="Edit" @click="openEdit(selectedItem)">编辑</el-button>
        </div>
        <div class="detail-poster">
          <el-image
            v-if="selectedItem.poster_url"
            :src="cleanPoster(selectedItem.poster_url)"
            :preview-src-list="[cleanPoster(selectedItem.poster_url)]"
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

        <div class="detail-screenshots" v-if="screenshotUrlList(selectedItem).length > 0">
          <div class="detail-screenshots-head">
            <span class="detail-label">视频截图</span>
            <el-button text :icon="CopyDocument" title="复制截图" @click="copyText(selectedItem.screenshots, '视频截图')" />
          </div>
          <div class="detail-screenshot-gallery">
            <img
              v-for="(url, index) in screenshotUrlList(selectedItem)"
              :key="'detail-screenshot-' + index"
              :src="cleanPoster(url)"
              :alt="'截图 ' + (index + 1)"
              class="detail-screenshot-img"
              @error="handleScreenshotError($event, url)"
            />
          </div>
        </div>
      </div>
    </el-dialog>

    <el-dialog
      v-model="editVisible"
      title="编辑资源信息"
      width="85%"
      append-to-body
      class="resource-edit-dialog"
    >
      <el-form :model="editForm" label-width="88px">
        <el-form-item label="标题">
          <el-input v-model="editForm.title" placeholder="标题" />
        </el-form-item>
        <el-form-item label="年份">
          <el-input v-model="editForm.year" placeholder="年份" />
        </el-form-item>
        <el-form-item label="国家">
          <el-input v-model="editForm.country" placeholder="国家" />
        </el-form-item>
        <el-form-item label="豆瓣ID">
          <el-input v-model="editForm.douban_id" placeholder="豆瓣ID" />
        </el-form-item>
        <el-form-item label="IMDbID">
          <el-input v-model="editForm.imdb_id" placeholder="IMDbID" />
        </el-form-item>
        <el-form-item label="TMDbID">
          <el-input v-model="editForm.tmdb_id" placeholder="TMDbID" />
        </el-form-item>
        <el-form-item label="海报地址">
          <el-input v-model="editForm.poster_url" type="textarea" :rows="2" placeholder="海报地址，支持 [img]url[/img]" />
        </el-form-item>
        <el-form-item label="简介">
          <el-input v-model="editForm.summary" type="textarea" :rows="4" placeholder="简介" />
        </el-form-item>
        <el-form-item label="视频截图">
          <el-input v-model="editForm.screenshots" type="textarea" :rows="4" placeholder="[img]url1[/img][img]url2[/img]" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="editSaving" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'
import { CopyDocument, View, Link, Edit } from '@element-plus/icons-vue'

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
  screenshots: string
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

const editVisible = ref(false)
const editSaving = ref(false)
const editForm = ref({
  id: 0,
  title: '',
  year: '',
  country: '',
  douban_id: '',
  imdb_id: '',
  tmdb_id: '',
  poster_url: '',
  summary: '',
  screenshots: '',
})

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

function openEdit(row: ResourceInfoItem) {
  detailVisible.value = false
  editForm.value = {
    id: row.id,
    title: row.title || '',
    year: row.year || '',
    country: row.country || '',
    douban_id: row.douban_id || '',
    imdb_id: row.imdb_id || '',
    tmdb_id: row.tmdb_id || '',
    poster_url: row.poster_url || '',
    summary: row.summary || '',
    screenshots: row.screenshots || '',
  }
  editVisible.value = true
}

async function saveEdit() {
  if (editForm.value.id <= 0) {
    ElMessage.error('资源 ID 无效')
    return
  }
  editSaving.value = true
  try {
    const payload = {
      title: editForm.value.title,
      year: editForm.value.year,
      country: editForm.value.country,
      douban_id: editForm.value.douban_id,
      imdb_id: editForm.value.imdb_id,
      tmdb_id: editForm.value.tmdb_id,
      poster_url: editForm.value.poster_url,
      summary: editForm.value.summary,
      screenshots: editForm.value.screenshots,
    }
    const response = await axios.put(`/api/resource_info/${editForm.value.id}`, payload)
    if (response.data && response.data.success) {
      ElMessage.success('保存成功')
      editVisible.value = false
      fetchList()
    } else {
      ElMessage.error('保存失败: ' + (response.data?.error || '未知错误'))
    }
  } catch (error) {
    const message = axios.isAxiosError(error) ? error.message : String(error)
    ElMessage.error('保存失败: ' + message)
  } finally {
    editSaving.value = false
  }
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
  ]
  copyText(lines.join('\n'), '全部信息')
}

function openPoster(url: string) {
  if (!url) {
    ElMessage.warning('海报地址为空，无法打开')
    return
  }
  window.open(cleanPoster(url), '_blank', 'noopener')
}

// cleanPoster 将 BBCode 形式 [img]url[/img] 的海报文本剥出纯 URL，避免 <img src> 失效。
function cleanPoster(raw: string): string {
  if (!raw) return ''
  const m = raw.match(/\[img(?:\=[^\]]*)?\]([\s\S]*?)\[\/img\]/i)
  if (m && m[1] && m[1].trim()) return m[1].trim()
  return raw
}

// screenshotUrlList 从资源信息库中的视频截图 BBCode 文本提取图片直链列表。
function screenshotUrlList(item: ResourceInfoItem | null): string[] {
  if (!item || !item.screenshots) return []
  const text = String(item.screenshots).trim()
  if (!text) return []
  const matches = [...text.matchAll(/\[img[^\]]*\](https?:\/\/[^\s[\]]+)\[\/img\]/gi)].map((m) => m[1])
  return matches.length > 0 ? matches : []
}

// handleScreenshotError 截图加载失败时隐藏，避免破图占位影响阅读。
function handleScreenshotError(event: Event, url: string) {
  const img = event.target as HTMLImageElement | null
  if (!img) return
  if (img.dataset.fallback === '1') {
    img.style.display = 'none'
    return
  }
  img.dataset.fallback = '1'
  img.src = url
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

/* 查看详情弹框：固定高度 900px（小屏用 max-height 兜底不超视口），
   弹框内用 flex 纵向布局，header 不收缩，超出的内容在 body 内滚动。 */
.resource-detail-dialog {
  max-height: 85%; /* 用视口单位适配不同屏幕，内容少的时候自动收缩 */
  overflow-y: auto;
  padding-right: 10px; /* 预留滚动条空间，避免内容抖动 */
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
  flex-wrap: wrap;
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

.detail-screenshots {
  flex-basis: 100%;
  margin-top: 12px;
}

.detail-screenshots-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.detail-screenshot-gallery {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.detail-screenshot-img {
  width: 160px;
  height: 90px;
  object-fit: cover;
  border-radius: 4px;
  border: 1px solid var(--el-border-color-lighter);
}

.break-all {
  word-break: break-all;
}
</style>
