<template>
  <el-dialog
    :model-value="visible"
    title="选择种子"
    width="95%"
    top="3vh"
    :close-on-click-modal="false"
    @update:model-value="$emit('update:visible', $event)"
    @open="handleOpen"
    class="seed-select-dialog"
  >
    <!-- 搜索栏和统计 -->
    <div class="seed-toolbar">
      <el-input
        v-model="searchQuery"
        placeholder="搜索标题或种子ID..."
        clearable
        style="width: 260px; margin-right: 12px"
        @input="handleSearchInput"
      />
      <el-select
        v-model="downloaderFilter"
        placeholder="按下载器筛选"
        clearable
        style="width: 180px; margin-right: 12px"
        @change="handleDownloaderChange"
      >
        <el-option
          v-for="dl in downloaderList"
          :key="dl.id"
          :label="dl.name"
          :value="dl.id"
        />
      </el-select>
      <div v-if="hasActiveFilters" style="display: flex; align-items: center; margin-right: 12px">
        <el-tag type="info" size="default" effect="plain">{{ currentFilterText }}</el-tag>
        <el-button type="danger" link style="padding: 0; margin-left: 8px" @click="clearAllFilters">清除</el-button>
      </div>
      <el-button type="primary" plain @click="openFilterDialog" style="margin-right: 12px">筛选</el-button>
      <el-tag type="info" effect="plain" style="margin-right: 12px">
        已选 {{ selectedSeeds.length }} 个种子
      </el-tag>
      <div style="flex: 1" />
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
          small
        />
      </div>
    </div>

    <!-- 种子表格 -->
    <div class="seed-table-container" :class="{ 'seed-table-container--scroll': tableShouldScroll }">
      <el-table
        ref="tableRef"
        :data="tableData"
        v-loading="loading"
        border
        style="width: 100%"
        :height="seedTableHeight"
        empty-text="暂无种子数据"
        class="glass-table"
        @selection-change="handleSelectionChange"
        @sort-change="handleSortChange"
        @expand-change="handleExpandChange"
        @row-click="handleRowClick"
        row-key="row_key"
      >
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="expand-sites-container">
              <div v-if="seedSitesLoading.has(row.row_key)" class="expand-sites-loading">
                <el-icon class="is-loading"><Loading /></el-icon>
                <span>加载站点信息...</span>
              </div>
              <template v-else-if="seedSitesCache.get(row.row_key)?.length">
                <div class="expand-sites-label">已发布到以下站点：</div>
                <div class="expand-sites-buttons">
                  <el-button
                    v-for="site in seedSitesCache.get(row.row_key)"
                    :key="site"
                    type="success"
                    size="small"
                    class="site-button"
                  >{{ site }}</el-button>
                </div>
              </template>
              <div v-else class="expand-sites-empty">暂无发布站点记录</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column
          type="selection"
          width="55"
          align="center"
          :reserve-selection="true"
        />
        <el-table-column prop="torrent_id" label="种子ID" width="80" align="center" show-overflow-tooltip />
        <el-table-column prop="nickname" label="站点" width="100" align="center">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ row.nickname || row.site_name }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="title" label="标题" min-width="260" sortable="custom">
          <template #default="{ row }">
            <div class="title-cell">
              <div class="subtitle-line" :title="row.subtitle">{{ row.subtitle || '' }}</div>
              <div class="main-title-line" :title="row.title">{{ row.title || '' }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="site_count" label="做种数" width="95" align="center" sortable="custom">
          <template #default="{ row }">
            <span :class="{ 'seeders-zero': !row.site_count }">
              {{ row.site_count ?? '-' }} / {{ row.total_site_count ?? '-' }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="下载器" width="140" align="center">
          <template #default="{ row }">
            <template v-if="row.downloader_names && row.downloader_names.length > 0">
              <el-tag
                v-for="name in row.downloader_names"
                :key="name"
                size="small"
                type="success"
                style="margin: 2px"
              >{{ name }}</el-tag>
            </template>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="size" label="大小" width="95" align="center" sortable="custom">
          <template #default="{ row }">
            {{ row.size != null ? formatSize(row.size) : '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="progress" label="进度" width="80" align="center" sortable="custom">
          <template #default="{ row }">
            <span v-if="row.progress != null" :class="getProgressClass(row.progress)">
              {{ formatProgress(row.progress) }}
            </span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 筛选器弹窗 -->
    <div v-if="filterDialogVisible" class="filter-overlay" @click.self="filterDialogVisible = false">
      <el-card class="filter-card">
        <template #header>
          <div class="filter-card-header">
            <span>筛选选项</span>
            <el-button type="danger" circle @click="filterDialogVisible = false" plain>X</el-button>
          </div>
        </template>
        <div class="filter-card-body">
          <el-divider content-position="left">站点筛选</el-divider>
          <div class="site-filter-container">
            <div style="display: flex; align-items: center; gap: 15px; margin-bottom: 5px">
              <el-radio-group v-model="siteFilterMode" size="default">
                <el-radio-button label="exist" class="compact-radio-button">存在于</el-radio-button>
                <el-radio-button label="not-exist" class="compact-radio-button">不存在于</el-radio-button>
              </el-radio-group>
              <el-input
                v-model="siteSearch"
                placeholder="搜索站点"
                clearable
                style="width: 240px; font-size: 14px"
                size="default"
              />
              <div style="display: flex; align-items: center; gap: 10px">
                <div v-if="tempFilters.existSites.length > 0" style="display: flex; align-items: center">
                  <el-tag type="info" size="default" effect="plain">存在于: {{ tempFilters.existSites.length }}</el-tag>
                  <el-button type="danger" link style="padding: 0; margin-left: 5px" @click="tempFilters.existSites = []">清除</el-button>
                </div>
                <div v-if="tempFilters.notExistSites.length > 0" style="display: flex; align-items: center">
                  <el-tag type="info" size="default" effect="plain">不存在于: {{ tempFilters.notExistSites.length }}</el-tag>
                  <el-button type="danger" link style="padding: 0; margin-left: 5px" @click="tempFilters.notExistSites = []">清除</el-button>
                </div>
              </div>
            </div>
            <div class="site-checkbox-container">
              <el-checkbox-group v-model="currentSiteNames">
                <el-checkbox
                  v-for="site in filteredSiteOptions"
                  :key="site"
                  :label="site"
                  :disabled="!isSiteAvailable(site)"
                  :class="{ 'disabled-site': !isSiteAvailable(site) }"
                >{{ site }}</el-checkbox>
              </el-checkbox-group>
            </div>
          </div>

          <el-divider content-position="left">状态</el-divider>
          <div style="margin-bottom: 10px">
            <div v-if="tempFilters.states.length > 0" style="display: flex; align-items: center">
              <el-tag type="info" size="default" effect="plain">状态: {{ tempFilters.states.length }}</el-tag>
              <el-button type="danger" link style="padding: 0; margin-left: 5px" @click="tempFilters.states = []">清除</el-button>
            </div>
          </div>
          <el-checkbox-group v-model="tempFilters.states">
            <el-checkbox v-for="state in uniqueStates" :key="state" :label="state">{{ state }}</el-checkbox>
          </el-checkbox-group>

          <el-divider content-position="left">保存路径</el-divider>
          <div style="margin-bottom: 10px">
            <div v-if="tempFilters.paths.length > 0" style="display: flex; align-items: center">
              <el-tag type="info" size="default" effect="plain">路径: {{ tempFilters.paths.length }}</el-tag>
              <el-button type="danger" link style="padding: 0; margin-left: 5px" @click="clearPathTree">清除</el-button>
            </div>
          </div>
          <div class="path-tree-container">
            <el-tree
              ref="pathTreeRef"
              :data="pathTreeData"
              show-checkbox
              node-key="path"
              default-expand-all
              :expand-on-click-node="false"
              check-on-click-node
              :check-strictly="true"
            />
          </div>
        </div>
        <div class="filter-card-footer">
          <el-button @click="clearTempFilters">清除筛选</el-button>
          <el-button @click="filterDialogVisible = false">取消</el-button>
          <el-button type="primary" @click="applyFilters">确认</el-button>
        </div>
      </el-card>
    </div>

    <template #footer>
      <el-button @click="$emit('update:visible', false)">取消</el-button>
      <el-button type="primary" @click="handleConfirm">
        确认选择 ({{ selectedSeeds.length }})
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, computed, reactive } from 'vue'
import type { TableInstance, Sort } from 'element-plus'
import type { ElTree } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import axios from 'axios'

type SeedRow = {
  torrent_id: string
  site_name: string
  name?: string
  nickname?: string
  title?: string
  subtitle?: string
  team?: string
  source?: string
  tags?: string
  site_count?: number
  total_site_count?: number
  downloader_ids?: string
  downloader_names?: string[]
  size?: number
  progress?: number
  row_key: string
}

type SelectedSeed = {
  torrent_id: string
  site_name: string
  title: string
}

type DownloaderItem = {
  id: string
  name: string
  color?: string
}

interface FilterState {
  existSites: string[]
  notExistSites: string[]
  states: string[]
  paths: string[]
}

interface PathNode {
  path: string
  label: string
  children?: PathNode[]
}

const props = defineProps<{
  visible: boolean
  initialSelection: SelectedSeed[]
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  confirm: [seeds: SelectedSeed[]]
}>()

const tableRef = ref<TableInstance>()
const loading = ref(false)
const tableData = ref<SeedRow[]>([])
const total = ref(0)
const currentPage = ref(1)
const pageSize = ref(50)
const searchQuery = ref('')
const selectedSeeds = ref<SelectedSeed[]>([])
const downloaderFilter = ref('')
const downloaderList = ref<DownloaderItem[]>([])
const tableShouldScroll = computed(() => tableData.value.length > 20)
const seedTableHeight = computed(() => (tableShouldScroll.value ? 'calc(85vh - 178px)' : undefined))

// 筛选相关
const filterDialogVisible = ref(false)
const siteFilterMode = ref<'exist' | 'not-exist'>('exist')
const siteSearch = ref('')
const allSites = ref<string[]>([])
const uniqueStates = ref<string[]>([])
const uniquePaths = ref<string[]>([])
const pathTreeRef = ref<InstanceType<typeof ElTree> | null>(null)
const pathTreeData = ref<PathNode[]>([])

const activeFilters = reactive<FilterState>({
  existSites: [],
  notExistSites: [],
  states: [],
  paths: [],
})

const tempFilters = reactive<FilterState>({
  existSites: [],
  notExistSites: [],
  states: [],
  paths: [],
})

// 排序
type SortOrder = 'ascending' | 'descending' | null
const currentSort = ref<{ prop: string; order: SortOrder }>({ prop: '', order: null })

let searchTimer: ReturnType<typeof setTimeout> | null = null

// 下载器 ID → 名称映射
const downloaderMap = new Map<string, string>()

// 展开行站点数据缓存
const seedSitesCache = new Map<string, string[]>()
const seedSitesLoading = ref<Set<string>>(new Set())

// 站点筛选的计算属性
const sortedSites = computed(() => {
  const collator = new Intl.Collator('zh-CN', { numeric: true })
  return [...allSites.value].sort(collator.compare)
})

const filteredSiteOptions = computed(() => {
  if (!siteSearch.value) return sortedSites.value
  const kw = siteSearch.value.toLowerCase()
  return sortedSites.value.filter(s => s.toLowerCase().includes(kw))
})

const currentSiteNames = computed({
  get: () => {
    return siteFilterMode.value === 'exist'
      ? tempFilters.existSites
      : tempFilters.notExistSites
  },
  set: (val) => {
    if (siteFilterMode.value === 'exist') {
      tempFilters.existSites = val
    } else {
      tempFilters.notExistSites = val
    }
  },
})

const isSiteAvailable = (site: string): boolean => {
  if (siteFilterMode.value === 'exist') {
    return !tempFilters.notExistSites.includes(site)
  } else {
    return !tempFilters.existSites.includes(site)
  }
}

const hasActiveFilters = computed(() => {
  return (
    activeFilters.existSites.length > 0 ||
    activeFilters.notExistSites.length > 0 ||
    activeFilters.states.length > 0 ||
    activeFilters.paths.length > 0
  )
})

const currentFilterText = computed(() => {
  const parts: string[] = []
  if (activeFilters.existSites.length > 0) parts.push(`存在于: ${activeFilters.existSites.length}`)
  if (activeFilters.notExistSites.length > 0) parts.push(`不存在于: ${activeFilters.notExistSites.length}`)
  if (activeFilters.states.length > 0) parts.push(`状态: ${activeFilters.states.length}`)
  if (activeFilters.paths.length > 0) parts.push(`路径: ${activeFilters.paths.length}`)
  return parts.join(', ')
})

const buildPathTree = (paths: string[]): PathNode[] => {
  const root: PathNode[] = []
  const nodeMap = new Map<string, PathNode>()
  paths.sort().forEach((fullPath) => {
    const parts = fullPath.replace(/^\/|\/$/g, '').split('/')
    let currentPath = ''
    let parentChildren = root
    parts.forEach((part, index) => {
      currentPath = index === 0 ? `/${part}` : `${currentPath}/${part}`
      if (!nodeMap.has(currentPath)) {
        const newNode: PathNode = {
          path: index === parts.length - 1 ? fullPath : currentPath,
          label: part,
          children: [],
        }
        nodeMap.set(currentPath, newNode)
        parentChildren.push(newNode)
      }
      const currentNode = nodeMap.get(currentPath)!
      parentChildren = currentNode.children!
    })
  })
  nodeMap.forEach((node) => {
    if (node.children && node.children.length === 0) {
      delete node.children
    }
  })
  return root
}

const fetchDownloaders = async () => {
  try {
    const res = await axios.get('/api/all_downloaders')
    const list: DownloaderItem[] = Array.isArray(res.data) ? res.data : []
    downloaderList.value = list
    downloaderMap.clear()
    for (const dl of list) {
      downloaderMap.set(dl.id, dl.name)
    }
  } catch (e) {
    console.error('获取下载器列表失败:', e)
  }
}

const resolveDownloaderNames = (idsStr: string | null | undefined): string[] => {
  if (!idsStr) return []
  return idsStr
    .split(',')
    .map(id => id.trim())
    .filter(Boolean)
    .map(id => downloaderMap.get(id) || id)
}

const formatSize = (bytes: number): string => {
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const val = bytes / Math.pow(1024, i)
  return `${val.toFixed(i > 1 ? 2 : 0)} ${units[i]}`
}

const formatProgress = (progress: number): string => {
  return `${progress.toFixed(1)}%`
}

const getProgressClass = (progress: number): string => {
  if (progress >= 100) return 'progress-done'
  if (progress > 0) return 'progress-partial'
  return 'progress-zero'
}

// 根据初始选择恢复勾选状态
const restoreSelection = async () => {
  await nextTick()
  tableRef.value?.clearSelection()
  if (selectedSeeds.value.length > 0 && tableData.value.length > 0) {
    const selectedKeys = new Set(selectedSeeds.value.map(s => `${s.site_name}:${s.torrent_id}`))
    for (const row of tableData.value) {
      if (selectedKeys.has(row.row_key)) {
        tableRef.value?.toggleRowSelection(row, true)
      }
    }
  }
}

const fetchData = async () => {
  loading.value = true
  try {
    const params = new URLSearchParams({
      page: String(currentPage.value),
      page_size: String(pageSize.value),
      search: searchQuery.value.trim(),
      downloader: downloaderFilter.value,
    })

    if (activeFilters.existSites.length > 0) {
      params.set('exist_sites', JSON.stringify(activeFilters.existSites))
    }
    if (activeFilters.notExistSites.length > 0) {
      params.set('not_exist_sites', JSON.stringify(activeFilters.notExistSites))
    }
    if (activeFilters.states.length > 0) {
      params.set('state_filters', JSON.stringify(activeFilters.states))
    }
    if (activeFilters.paths.length > 0) {
      params.set('path_filters', JSON.stringify(activeFilters.paths))
    }
    if (currentSort.value.prop && currentSort.value.order) {
      params.set('sort_prop', currentSort.value.prop)
      params.set('sort_order', currentSort.value.order)
    }

    const res = await axios.get(`/api/scheduled-seed/seeds?${params.toString()}`)
    if (res.data?.success) {
      const raw = Array.isArray(res.data.data) ? res.data.data : []
      tableData.value = raw.map((s: Record<string, unknown>) => ({
        ...s,
        downloader_names: resolveDownloaderNames(s.downloader_ids as string | null | undefined),
        row_key: `${s.site_name}:${s.torrent_id}`,
      }))
      total.value = Number(res.data.total || 0)

      // 更新筛选项元数据
      if (Array.isArray(res.data.all_sites)) {
        allSites.value = res.data.all_sites
      }
      if (Array.isArray(res.data.unique_states)) {
        uniqueStates.value = res.data.unique_states
      }
      if (Array.isArray(res.data.unique_paths)) {
        uniquePaths.value = res.data.unique_paths
        pathTreeData.value = buildPathTree(res.data.unique_paths)
      }

      await restoreSelection()
    }
  } catch (e) {
    console.error('获取种子列表失败:', e)
  } finally {
    loading.value = false
  }
}

const handleSearchInput = () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    currentPage.value = 1
    fetchData()
  }, 300)
}

const handleDownloaderChange = () => {
  currentPage.value = 1
  fetchData()
}

const handleSizeChange = () => {
  currentPage.value = 1
  fetchData()
}

const handleCurrentChange = () => {
  fetchData()
}

const handleSortChange = (sort: { prop: string | null; order: SortOrder }) => {
  currentSort.value = {
    prop: sort.prop || '',
    order: sort.order,
  }
  currentPage.value = 1
  fetchData()
}

const handleSelectionChange = (rows: SeedRow[]) => {
  // 合并：保留其他页面的选中项，更新当前页面的选中项
  const currentPageKeys = new Set(tableData.value.map(r => r.row_key))
  const otherPageSelections = selectedSeeds.value.filter(
    s => !currentPageKeys.has(`${s.site_name}:${s.torrent_id}`)
  )
  const currentPageSelections: SelectedSeed[] = rows.map(r => ({
    torrent_id: r.torrent_id,
    site_name: r.site_name,
    title: r.title || '',
  }))
  selectedSeeds.value = [...otherPageSelections, ...currentPageSelections]
}

// 展开行：加载种子发布站点
const handleExpandChange = async (row: SeedRow, expandedRows: SeedRow[]) => {
  const isExpanding = expandedRows.some(r => r.row_key === row.row_key)
  if (!isExpanding) return

  const cacheKey = row.row_key
  if (seedSitesCache.has(cacheKey)) return

  if (!row.name) {
    seedSitesCache.set(cacheKey, [])
    return
  }

  const loadingSet = new Set(seedSitesLoading.value)
  loadingSet.add(cacheKey)
  seedSitesLoading.value = loadingSet

  try {
    const res = await axios.get('/api/scheduled-seed/seed-sites', { params: { name: row.name } })
    if (res.data?.success && Array.isArray(res.data.data)) {
      seedSitesCache.set(cacheKey, res.data.data)
    } else {
      seedSitesCache.set(cacheKey, [])
    }
  } catch {
    seedSitesCache.set(cacheKey, [])
  } finally {
    const newSet = new Set(seedSitesLoading.value)
    newSet.delete(cacheKey)
    seedSitesLoading.value = newSet
  }
}

// 点击行触发展开（排除复选框点击）
const handleRowClick = (row: SeedRow, _column: unknown, event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (target.closest('.el-checkbox') || target.closest('.el-checkbox__label')) return
  tableRef.value?.toggleRowExpansion(row)
}

// 筛选相关方法
const openFilterDialog = () => {
  tempFilters.existSites = [...activeFilters.existSites]
  tempFilters.notExistSites = [...activeFilters.notExistSites]
  tempFilters.states = [...activeFilters.states]
  tempFilters.paths = [...activeFilters.paths]
  filterDialogVisible.value = true
  nextTick(() => {
    if (pathTreeRef.value) {
      pathTreeRef.value.setCheckedKeys(activeFilters.paths, false)
    }
  })
}

const applyFilters = () => {
  if (pathTreeRef.value) {
    const selectedPaths = pathTreeRef.value.getCheckedKeys(false)
    tempFilters.paths = selectedPaths as string[]
  }
  activeFilters.existSites = [...tempFilters.existSites]
  activeFilters.notExistSites = [...tempFilters.notExistSites]
  activeFilters.states = [...tempFilters.states]
  activeFilters.paths = [...tempFilters.paths]
  filterDialogVisible.value = false
  currentPage.value = 1
  fetchData()
}

const clearTempFilters = () => {
  tempFilters.existSites = []
  tempFilters.notExistSites = []
  tempFilters.states = []
  tempFilters.paths = []
  siteFilterMode.value = 'exist'
  siteSearch.value = ''
  if (pathTreeRef.value) {
    pathTreeRef.value.setCheckedKeys([], false)
  }
}

const clearPathTree = () => {
  tempFilters.paths = []
  if (pathTreeRef.value) {
    pathTreeRef.value.setCheckedKeys([], false)
  }
}

const clearAllFilters = () => {
  activeFilters.existSites = []
  activeFilters.notExistSites = []
  activeFilters.states = []
  activeFilters.paths = []
  currentPage.value = 1
  fetchData()
}

const handleOpen = () => {
  // 从 initialSelection 恢复
  selectedSeeds.value = [...props.initialSelection]
  currentPage.value = 1
  searchQuery.value = ''
  downloaderFilter.value = ''
  currentSort.value = { prop: '', order: null }
  activeFilters.existSites = []
  activeFilters.notExistSites = []
  activeFilters.states = []
  activeFilters.paths = []
  seedSitesCache.clear()
  seedSitesLoading.value = new Set()
  fetchDownloaders()
  fetchData()
}

const handleConfirm = () => {
  emit('confirm', [...selectedSeeds.value])
  emit('update:visible', false)
}

watch(() => props.visible, (val) => {
  if (!val) {
    // 关闭时清理选择状态
    tableRef.value?.clearSelection()
  }
})
</script>

<style scoped>
.seed-select-dialog :deep(.el-dialog) {
  height: 85vh;
  display: flex;
  flex-direction: column;
}

.seed-select-dialog :deep(.el-dialog__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding-top: 10px;
}

.seed-toolbar {
  display: flex;
  align-items: center;
  margin-bottom: 12px;
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 4px 0;
}

.seed-table-container {
  flex: 1;
  overflow: hidden;
  min-height: 0;
}

.seed-table-container--scroll {
  max-height: calc(85vh - 178px);
}

.seed-table-container--scroll :deep(.el-table__body-wrapper) {
  overflow-y: auto;
}

.title-cell {
  line-height: 1.4;
}

.title-cell .subtitle-line {
  font-size: 12px;
  color: #909399;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.title-cell .main-title-line {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.seeders-zero {
  color: #c0c4cc;
}

.text-muted {
  color: #c0c4cc;
}

.progress-done {
  color: #67c23a;
  font-weight: 600;
}

.progress-partial {
  color: #e6a23c;
}

.progress-zero {
  color: #c0c4cc;
}

.pagination-controls {
  display: flex;
  align-items: center;
}

/* 筛选弹窗样式 */
.filter-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(0, 0, 0, 0.5);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 2100;
}

.filter-card {
  width: 800px;
  max-width: 95vw;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
}

.filter-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

:deep(.filter-card .el-card__body) {
  padding: 0;
  flex: 1;
  overflow-y: auto;
}

:deep(.el-card__header) {
  padding: 5px 10px;
}

:deep(.el-divider--horizontal) {
  margin: 18px 0;
}

.filter-card-body {
  overflow-y: auto;
  padding: 10px 15px;
}

.filter-card-footer {
  padding: 5px 10px;
  border-top: 1px solid var(--el-border-color-lighter);
  display: flex;
  justify-content: flex-end;
}

.filter-card .el-checkbox-group,
.filter-card .el-radio-group {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 0;
}

.filter-card .el-checkbox,
.filter-card .el-radio {
  margin-right: 15px !important;
}

.site-filter-container {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
}

.site-checkbox-container {
  width: 100%;
  height: 160px;
  overflow-y: auto;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 10px;
  margin-top: 10px;
  box-sizing: border-box;
}

:deep(.site-checkbox-container .el-checkbox-group) {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 8px;
}

:deep(.site-checkbox-container .el-checkbox) {
  margin-right: 0 !important;
}

.disabled-site {
  opacity: 0.5;
  text-decoration: line-through;
}

.disabled-site :deep(.el-checkbox__input.is-disabled) {
  opacity: 0.5;
}

.compact-radio-button :deep(.el-radio-button__inner) {
  font-size: 14px;
  padding: 8px 20px;
  border-radius: 0;
}

.compact-radio-button:first-child :deep(.el-radio-button__inner) {
  border-top-left-radius: 4px;
  border-bottom-left-radius: 4px;
}

.compact-radio-button:last-child :deep(.el-radio-button__inner) {
  border-top-right-radius: 4px;
  border-bottom-right-radius: 4px;
  margin-left: -1px;
}

.path-tree-container {
  max-height: 200px;
  overflow-y: auto;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 5px;
}

:deep(.path-tree-node .el-tree-node__content) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 展开行站点显示 */
.expand-sites-container {
  padding: 12px 20px;
}

.seed-select-dialog :deep(.glass-table .el-table__body tr) {
  cursor: pointer;
}

.expand-sites-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #909399;
  font-size: 13px;
}

.expand-sites-label {
  font-size: 13px;
  color: #606266;
  margin-bottom: 8px;
}

.expand-sites-buttons {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.expand-sites-buttons .site-button {
  min-width: 60px;
}

.expand-sites-empty {
  color: #c0c4cc;
  font-size: 13px;
}
</style>
