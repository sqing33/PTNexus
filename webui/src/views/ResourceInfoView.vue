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
        <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip />
        <el-table-column prop="year" label="年份" width="76">
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.year }">{{ row.year || 'N/A' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="country" label="国家" width="110" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.country }">{{ row.country || 'N/A' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="豆瓣ID" width="110">
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.douban_id }">{{ row.douban_id || 'N/A' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="IMDbID" width="130">
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.imdb_id }">{{ row.imdb_id || 'N/A' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="TMDbID" width="110">
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.tmdb_id }">{{ row.tmdb_id || 'N/A' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="summary" label="简介" min-width="240" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.summary }">{{ row.summary || 'N/A' }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="updated_at" label="更新时间" width="160">
          <template #default="{ row }">
            <span :class="{ 'empty-text': !row.updated_at }">{{ row.updated_at || 'N/A' }}</span>
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
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'

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

.empty-text {
  color: var(--el-text-color-placeholder);
}

.pagination-container {
  display: flex;
  justify-content: flex-end;
  margin-top: 15px;
}
</style>
