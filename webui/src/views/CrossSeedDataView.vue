<template>
  <div class="cross-seed-data-view">
    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      style="margin: 0; border-radius: 0"
    ></el-alert>

    <div class="search-and-controls glass-table">
      <el-input
        v-model="searchQuery"
        placeholder="搜索标题或种子ID..."
        clearable
        class="search-input"
        style="width: 300px; margin-right: 15px"
      />

      <el-button
        v-if="isCrossSeedMode"
        type="success"
        @click="handleBatchCrossSeedButtonClick"
        plain
        style="margin-right: 15px"
      >
        {{ batchCrossSeedButtonText }}
      </el-button>

      <el-button
        v-if="isMaintenanceMode"
        type="info"
        @click="openRecordViewDialog"
        plain
        style="margin-right: 15px"
      >
        BDInfo记录
      </el-button>

      <el-button
        v-if="isMaintenanceMode"
        type="warning"
        @click="openBatchFetchDialog"
        plain
        style="margin-right: 15px"
      >
        获取数据
      </el-button>

      <el-button
        v-if="isMaintenanceMode"
        type="danger"
        @click="isDeleteMode && selectedRows.length > 0 ? executeBatchDelete() : toggleDeleteMode()"
        plain
        style="margin-right: 15px"
      >
        {{ getDeleteButtonText() }}
      </el-button>

      <el-button type="primary" @click="openFilterDialog" plain style="margin-right: 15px">
        筛选
      </el-button>

      <el-radio-group
        v-if="isMaintenanceMode"
        v-model="reviewStatusFilter"
        @change="handleReviewStatusChange"
        style="margin-right: 15px"
      >
        <el-radio-button label="">全部</el-radio-button>
        <el-radio-button label="reviewed">已检查</el-radio-button>
        <el-radio-button label="unreviewed">待检查</el-radio-button>
        <el-radio-button label="error">错误</el-radio-button>
      </el-radio-group>

      <div
        v-if="hasActiveFilters"
        class="current-filters"
        style="margin-right: 15px; display: flex; align-items: center"
      >
        <el-tag type="info" size="default" effect="plain">{{ currentFilterText }}</el-tag>
        <el-button type="danger" link style="padding: 0; margin-left: 8px" @click="clearFilters"
          >清除</el-button
        >
      </div>

      <div class="pagination-controls" v-if="tableData.length > 0">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
          background
        >
        </el-pagination>
      </div>
    </div>

    <div v-if="filterDialogVisible" class="filter-overlay" @click.self="closeFilterDialog">
      <el-card class="filter-card">
        <template #header>
          <div class="filter-card-header">
            <span>筛选选项</span>
            <el-button type="danger" circle @click="closeFilterDialog" plain>X</el-button>
          </div>
        </template>
        <div class="filter-card-body">
          <el-divider content-position="left">保存路径</el-divider>
          <div v-loading="uniquePathsLoading" class="path-tree-container">
            <el-tree
              ref="pathTreeRef"
              :data="pathTreeData"
              show-checkbox
              node-key="path"
              default-expand-all
              :expand-on-click-node="false"
              check-on-click-node
              :check-strictly="true"
              :props="{ class: 'path-tree-node' }"
            />
          </div>

          <template v-if="isMaintenanceMode">
            <el-divider content-position="left">删除状态</el-divider>
            <el-radio-group v-model="tempFilters.isDeleted" style="width: 100%">
              <el-radio :label="''">全部</el-radio>
              <el-radio :label="'0'">未删除</el-radio>
              <el-radio :label="'1'">已删除</el-radio>
            </el-radio-group>
          </template>

          <template v-if="isCrossSeedMode">
            <el-divider content-position="left">
              <span class="target-site-filter-title">
                <el-icon><WarningFilled /></el-icon>
                <span>选择批量转种目标站点</span>
              </span>
            </el-divider>
            <div class="target-sites-container">
              <div class="selected-site-display">
                <div v-if="selectedTargetSite" class="selected-site-info">
                  <el-tag type="info" size="default" effect="plain"
                    >已选择: {{ selectedTargetSite }}</el-tag
                  >
                  <el-button
                    type="danger"
                    link
                    style="padding: 0; margin-left: 8px"
                    @click="clearSelectedTargetSite"
                    >清除</el-button
                  >
                </div>
                <div v-else class="selected-site-info">
                  <el-tag type="info" size="default" effect="plain">未选择</el-tag>
                </div>
              </div>
              <div class="target-sites-radio-container">
                <el-radio-group v-model="selectedTargetSite" class="target-sites-radio-group">
                  <el-radio
                    v-for="site in targetSitesList"
                    :key="site"
                    :label="site"
                    class="target-site-radio"
                  >
                    {{ site }}
                  </el-radio>
                </el-radio-group>
              </div>
            </div>
          </template>
        </div>
        <div class="filter-card-footer">
          <el-button @click="closeFilterDialog">取消</el-button>
          <el-button type="primary" @click="applyFilters">确认</el-button>
        </div>
      </el-card>
    </div>

    <div class="table-container">
      <el-table
        :data="tableData"
        v-loading="loading"
        border
        style="width: 100%"
        empty-text="暂无待处理数据"
        :max-height="tableMaxHeight"
        height="100%"
        :row-class-name="tableRowClassName"
        @selection-change="handleSelectionChange"
        class="glass-table"
      >
        <el-table-column
          v-if="isCrossSeedMode || (isMaintenanceMode && isDeleteMode)"
          type="selection"
          width="55"
          align="center"
          :selectable="checkSelectable"
        ></el-table-column>
        <el-table-column
          prop="torrent_id"
          label="种子ID"
          align="center"
          width="80"
          show-overflow-tooltip
        ></el-table-column>

        <el-table-column prop="nickname" label="站点名称" width="100" align="center">
          <template #default="scope">
            <div class="mapped-cell">{{ scope.row.nickname }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="title" label="标题" align="center">
          <template #default="scope">
            <div class="title-cell">
              <div class="subtitle-line" :title="scope.row.subtitle">
                {{ scope.row.subtitle || '' }}
              </div>
              <div class="main-title-line" :title="scope.row.title">
                {{ scope.row.title || '' }}
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="type" label="类型" width="100" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value': !isValidFormat(scope.row.type) || !isMapped('type', scope.row.type),
              }"
            >
              {{ getMappedValue('type', scope.row.type) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="medium" label="媒介" width="100" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.medium) || !isMapped('medium', scope.row.medium),
              }"
            >
              {{ getMappedValue('medium', scope.row.medium) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="video_codec" label="视频编码" width="120" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.video_codec) ||
                  !isMapped('video_codec', scope.row.video_codec),
              }"
            >
              {{ getMappedValue('video_codec', scope.row.video_codec) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="audio_codec" label="音频编码" width="90" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.audio_codec) ||
                  !isMapped('audio_codec', scope.row.audio_codec),
              }"
            >
              {{ getMappedValue('audio_codec', scope.row.audio_codec) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="resolution" label="分辨率" width="90" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.resolution) ||
                  !isMapped('resolution', scope.row.resolution),
              }"
            >
              {{ getMappedValue('resolution', scope.row.resolution) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="team" label="制作组" width="120" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value': !isValidFormat(scope.row.team) || !isMapped('team', scope.row.team),
              }"
            >
              {{ getMappedValue('team', scope.row.team) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="source" label="产地" width="100" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.source) || !isMapped('source', scope.row.source),
              }"
            >
              {{ getMappedValue('source', scope.row.source) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column
          prop="tags"
          label="标签"
          align="center"
          width="170"
          sortable
          :sort-by="getTagsSortKey"
        >
          <template #default="scope">
            <div class="tags-cell">
              <el-tag
                v-for="(tag, index) in getMappedTags(scope.row.tags)"
                :key="tag"
                size="small"
                :type="getTagType(scope.row.tags, index)"
                :class="getTagClass(scope.row.tags, index)"
                style="margin: 2px"
              >
                {{ tag }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="unrecognized" label="无法识别" width="120" align="center">
          <template #default="scope">
            <div class="mapped-cell" :class="{ 'invalid-value': scope.row.unrecognized }">
              {{ scope.row.unrecognized || '' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="updated_at" label="更新时间" width="140" align="center" sortable>
          <template #default="scope">
            <div class="mapped-cell datetime-cell">
              {{
                scope.row.is_deleted ? getRestrictionText(scope.row) : formatDateTime(scope.row.updated_at)
              }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110" align="center" fixed="right">
          <template #default="scope">
            <el-button
              size="small"
              :type="isMaintenanceMode ? 'primary' : 'success'"
              @click="isMaintenanceMode ? handleMaintain(scope.row) : handleSingleCrossSeed(scope.row)"
            >
              {{ isMaintenanceMode ? '维护' : '转种' }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <BDInfoRecordsDialog v-if="isMaintenanceMode" v-model="recordDialogVisible" @closed="fetchData" />

    <div v-if="isMaintenanceMode && batchFetchDialogVisible" class="modal-overlay">
      <el-card class="batch-fetch-main-card" shadow="always">
        <template #header>
          <div class="modal-header">
            <span>批量获取种子数据</span>
            <el-button type="danger" circle @click="closeBatchFetchDialog" plain>X</el-button>
          </div>
        </template>
        <div class="batch-fetch-main-content">
          <BatchFetchPanel
            ref="batchFetchPanelRef"
            @cancel="closeBatchFetchDialog"
            @fetch-completed="handleFetchCompleted"
          />
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { WarningFilled } from '@element-plus/icons-vue'
import { ElMessageBox } from 'element-plus'
import type { ElTree } from 'element-plus'
import axios from 'axios'
import BatchFetchPanel from '@/components/BatchFetchPanel.vue'
import BDInfoRecordsDialog from '@/components/cross-seed-data/BDInfoRecordsDialog.vue'
import '@/assets/styles/glass-morphism.scss'
import { ElMessage } from '@/utils/uiNotify'

interface SeedParameter {
  id: number
  hash: string
  torrent_id: string
  site_name: string
  nickname: string
  save_path: string
  downloader_id?: string
  title: string
  subtitle: string
  imdb_link: string
  douban_link: string
  type: string
  medium: string
  video_codec: string
  audio_codec: string
  resolution: string
  team: string
  source: string
  tags: string[] | string
  poster: string
  screenshots: string
  statement: string
  body: string
  mediainfo: string
  title_components: string
  unrecognized: string
  created_at: string
  updated_at: string
  is_deleted: boolean
  is_reviewed: boolean
}

interface PathNode {
  path: string
  label: string
  children?: PathNode[]
}

interface ReverseMappings {
  type: Record<string, string>
  medium: Record<string, string>
  video_codec: Record<string, string>
  audio_codec: Record<string, string>
  resolution: Record<string, string>
  source: Record<string, string>
  team: Record<string, string>
  tags: Record<string, string>
  site_name: Record<string, string>
}

const props = withDefaults(
  defineProps<{
    mode?: 'cross-seed' | 'maintenance'
  }>(),
  {
    mode: 'cross-seed',
  },
)

const emit = defineEmits<{
  (e: 'ready', refreshMethod: () => Promise<void>): void
  (e: 'maintain', row: SeedParameter): void
}>()

const router = useRouter()
const isMaintenanceMode = computed(() => props.mode === 'maintenance')
const isCrossSeedMode = computed(() => !isMaintenanceMode.value)

const isAnimationRelatedType = (typeValue: string | undefined | null) => {
  const text = (typeValue || '').trim().toLowerCase()
  if (!text) return false
  if (text === 'category.animation') {
    return true
  }
  return (
    text.includes('animation') ||
    text.includes('anime') ||
    text.includes('动漫') ||
    text.includes('动画')
  )
}

const reverseMappings = ref<ReverseMappings>({
  type: {},
  medium: {},
  video_codec: {},
  audio_codec: {},
  resolution: {},
  source: {},
  team: {},
  tags: {},
  site_name: {},
})

const tableData = ref<SeedParameter[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const selectedRows = ref<SeedParameter[]>([])
const pendingBatchCrossSeedAfterFilter = ref(false)
const batchFetchDialogVisible = ref(false)
const batchFetchPanelRef = ref<InstanceType<typeof BatchFetchPanel> | null>(null)
const isDeleteMode = ref(false)
const recordDialogVisible = ref(false)
const pathTreeRef = ref<InstanceType<typeof ElTree> | null>(null)
const pathTreeData = ref<PathNode[]>([])
const uniquePaths = ref<string[]>([])
const uniquePathsLoaded = ref(false)
const uniquePathsLoading = ref(false)

let fetchSequence = 0
let searchDebounceTimer: ReturnType<typeof window.setTimeout> | null = null

const tableMaxHeight = ref(window.innerHeight - 80)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const searchQuery = ref('')
const reviewStatusFilter = ref('')
const filterDialogVisible = ref(false)
const activeFilters = ref({
  paths: [] as string[],
  isDeleted: '',
  excludeTargetSites: '',
})
const tempFilters = ref({ ...activeFilters.value })
const targetSitesList = ref<string[]>([])
const uiInitializing = ref(true)

const handleReviewStatusChange = async (value: string) => {
  reviewStatusFilter.value = value
  currentPage.value = 1
  try {
    await axios.post('/api/config/cross_seed_review_filter', { review_filter: value })
  } catch (e) {
    console.error('保存检查状态筛选失败:', e)
  }
  await fetchData()
}

const currentFilterText = computed(() => {
  const filters = activeFilters.value
  const filterTexts = []

  if (reviewStatusFilter.value === 'reviewed') {
    filterTexts.push('已检查')
  } else if (reviewStatusFilter.value === 'unreviewed') {
    filterTexts.push('待检查')
  } else if (reviewStatusFilter.value === 'error') {
    filterTexts.push('错误')
  }

  if (filters.paths.length > 0) {
    filterTexts.push(`路径: ${filters.paths.length}`)
  }
  if (isMaintenanceMode.value && filters.isDeleted === '0') {
    filterTexts.push('未删除')
  } else if (isMaintenanceMode.value && filters.isDeleted === '1') {
    filterTexts.push('已删除')
  }
  if (isCrossSeedMode.value && filters.excludeTargetSites.trim() !== '') {
    filterTexts.push(`目标站点: ${filters.excludeTargetSites}`)
  }

  return filterTexts.join(', ')
})

const hasActiveFilters = computed(() => {
  const filters = activeFilters.value
  return (
    reviewStatusFilter.value !== '' ||
    filters.paths.length > 0 ||
    (isMaintenanceMode.value && filters.isDeleted !== '') ||
    (isCrossSeedMode.value && filters.excludeTargetSites.trim() !== '')
  )
})

const selectedTargetSite = computed({
  get: () => tempFilters.value.excludeTargetSites || '',
  set: (site) => {
    tempFilters.value.excludeTargetSites = site
  },
})

const clearSelectedTargetSite = () => {
  tempFilters.value.excludeTargetSites = ''
}

const batchCrossSeedButtonText = computed(() => {
  const selectedCount = selectedRows.value.length
  const targetSiteName = activeFilters.value.excludeTargetSites.trim()

  if (selectedCount === 0) {
    return '批量转种'
  }
  if (targetSiteName) {
    return `转种到 ${targetSiteName} (${selectedCount})`
  }
  return `批量转种 (${selectedCount})`
})

const getMappedValue = (category: keyof ReverseMappings, standardValue: string) => {
  if (!standardValue) return ''
  const mappings = reverseMappings.value[category]
  if (!mappings) return standardValue
  return mappings[standardValue] || standardValue
}

const isValidFormat = (value: string) => {
  if (!value) return true
  return /^[^.]+[.][^.]+$/.test(value)
}

const isMapped = (category: keyof ReverseMappings, standardValue: string) => {
  if (!standardValue) return true
  const mappings = reverseMappings.value[category]
  if (!mappings) return false
  return !!mappings[standardValue]
}

const compareTagAZ = (left: string, right: string) => {
  const leftLower = left.toLowerCase()
  const rightLower = right.toLowerCase()
  if (leftLower !== rightLower) {
    return leftLower < rightLower ? -1 : 1
  }
  if (left !== right) {
    return left < right ? -1 : 1
  }
  return 0
}

const normalizeTagList = (tags: string[] | string): string[] => {
  if (typeof tags === 'string') {
    try {
      return JSON.parse(tags)
    } catch {
      return tags
        .split(',')
        .map((tag) => tag.trim())
        .filter((tag) => tag)
    }
  }
  if (Array.isArray(tags)) {
    return tags
  }
  return []
}

const normalizeAndSortTags = (tags: string[] | string): string[] => {
  const tagList = normalizeTagList(tags)
    .map((tag) => String(tag || '').trim())
    .filter((tag) => tag)

  if (tagList.length <= 1) return tagList
  return [...tagList].sort(compareTagAZ)
}

const getTagsSortKey = (row: SeedParameter) => normalizeAndSortTags(row.tags).join('|').toLowerCase()

const getMappedTags = (tags: string[] | string) => {
  const tagList = normalizeAndSortTags(tags)
  if (tagList.length === 0) {
    return []
  }
  return tagList.map((tag: string) => reverseMappings.value.tags[tag] || tag)
}

const getTagType = (tags: string[] | string, index: number) => {
  const tagList = normalizeAndSortTags(tags)
  if (tagList.length === 0 || index >= tagList.length) return 'info'

  const originalTag = tagList[index]
  if (
    originalTag === '禁转' ||
    originalTag === 'tag.禁转' ||
    originalTag === '限转' ||
    originalTag === 'tag.限转' ||
    originalTag === '分集' ||
    originalTag === 'tag.分集'
  ) {
    return 'danger'
  }
  if (!isValidFormat(originalTag) || !isMapped('tags', originalTag)) {
    return 'danger'
  }
  return 'info'
}

const getTagClass = (tags: string[] | string, index: number) => {
  const tagList = normalizeAndSortTags(tags)
  if (tagList.length === 0 || index >= tagList.length) return ''

  const originalTag = tagList[index]
  if (
    originalTag === '禁转' ||
    originalTag === 'tag.禁转' ||
    originalTag === '限转' ||
    originalTag === 'tag.限转' ||
    originalTag === '分集' ||
    originalTag === 'tag.分集'
  ) {
    return 'restricted-tag'
  }
  if (!isValidFormat(originalTag) || !isMapped('tags', originalTag)) {
    return 'invalid-tag'
  }
  return ''
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

const hasInvalidParams = (row: SeedParameter): boolean => {
  const categories: (keyof Omit<ReverseMappings, 'tags' | 'site_name'>)[] = [
    'type',
    'medium',
    'video_codec',
    'audio_codec',
    'resolution',
    'team',
    'source',
  ]

  for (const category of categories) {
    const value = row[category as keyof SeedParameter] as string
    if (value && (!isValidFormat(value) || !isMapped(category, value))) {
      return true
    }
  }

  for (const tag of normalizeTagList(row.tags)) {
    if (!isValidFormat(tag) || !isMapped('tags', tag)) {
      return true
    }
  }

  return !!row.unrecognized
}

const hasRestrictedTag = (tags: string[] | string): boolean => {
  const tagList = normalizeTagList(tags)
  return tagList.some(
    (tag) =>
      tag === '禁转' ||
      tag === 'tag.禁转' ||
      tag === '限转' ||
      tag === 'tag.限转' ||
      tag === '分集' ||
      tag === 'tag.分集',
  )
}

const getRestrictionText = (row: SeedParameter) => {
  const labels: string[] = []

  if (row.is_deleted) {
    labels.push('已删除做种文件')
  }
  if (!row.is_reviewed) {
    labels.push('待检查')
  }
  if (hasRestrictedTag(row.tags)) {
    labels.push('禁转标签')
  }
  if (hasInvalidParams(row)) {
    labels.push('参数异常')
  }

  return labels.join('\n')
}

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

const checkSelectable = (row: SeedParameter) => {
  if (isMaintenanceMode.value && isDeleteMode.value) {
    return true
  }
  if (row.is_deleted) {
    return false
  }
  if (!row.is_reviewed) {
    return false
  }
  if (hasInvalidParams(row)) {
    return false
  }
  if (hasRestrictedTag(row.tags)) {
    return false
  }
  return true
}

const filterCrossSeedableRows = (rows: SeedParameter[]) => rows.filter((row) => checkSelectable(row))

const fetchData = async () => {
  const requestId = ++fetchSequence
  loading.value = true
  error.value = null
  try {
    const params = new URLSearchParams({
      page: currentPage.value.toString(),
      page_size: pageSize.value.toString(),
      search: searchQuery.value,
      path_filters: JSON.stringify(activeFilters.value.paths || []),
      is_deleted: isMaintenanceMode.value ? activeFilters.value.isDeleted : '',
      exclude_target_sites: isCrossSeedMode.value ? activeFilters.value.excludeTargetSites : '',
      review_status: isCrossSeedMode.value ? 'reviewed' : isMaintenanceMode.value ? reviewStatusFilter.value : '',
      include_unique_paths: '0',
    })

    const response = await axios.get(`/api/cross-seed-data?${params.toString()}`)
    const result = response.data

    if (requestId !== fetchSequence) {
      return
    }

    if (result.success) {
      const rawRows = Array.isArray(result.data) ? (result.data as SeedParameter[]) : []
      const visibleRows = rawRows
      tableData.value = visibleRows
      total.value = Number(result.total || rawRows.length)

      if (result.reverse_mappings) {
        reverseMappings.value = result.reverse_mappings
      }

      if (result.unique_paths) {
        uniquePaths.value = result.unique_paths
        pathTreeData.value = buildPathTree(result.unique_paths)
        uniquePathsLoaded.value = true
      }

      if (result.target_sites) {
        const filteredTargetSites = (result.target_sites || []).filter((site: string) => {
          const normalized = String(site || '')
            .trim()
            .toLowerCase()

          if (normalized !== 'ilolicon') {
            return true
          }

          return rawRows.some((row: SeedParameter) => isAnimationRelatedType(row.type))
        })

        targetSitesList.value = filteredTargetSites

        if (
          activeFilters.value.excludeTargetSites &&
          !filteredTargetSites.includes(activeFilters.value.excludeTargetSites)
        ) {
          activeFilters.value.excludeTargetSites = ''
        }
      }
    } else {
      error.value = result.error || '获取数据失败'
      ElMessage.error(result.error || '获取数据失败')
    }
  } catch (e: unknown) {
    if (requestId !== fetchSequence) {
      return
    }

    const message = axios.isAxiosError(e)
      ? (e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message
      : e instanceof Error
        ? e.message
        : '网络错误'
    error.value = message
    ElMessage.error(message)
  } finally {
    if (requestId === fetchSequence) {
      loading.value = false
    }
  }
}

const loadUniquePaths = async (force = false) => {
  if (uniquePathsLoading.value || (uniquePathsLoaded.value && !force)) {
    return
  }

  uniquePathsLoading.value = true
  try {
    const response = await axios.get('/api/cross-seed-data/unique-paths')
    const result = response.data

    if (!result.success) {
      throw new Error(result.error || '加载路径列表失败')
    }

    const paths = Array.isArray(result.unique_paths) ? result.unique_paths : []
    uniquePaths.value = paths
    pathTreeData.value = buildPathTree(paths)
    uniquePathsLoaded.value = true
  } catch (e) {
    const message = axios.isAxiosError(e)
      ? (e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message
      : e instanceof Error
        ? e.message
        : '加载路径列表失败'
    ElMessage.error(message)
  } finally {
    uniquePathsLoading.value = false
  }
}

const saveUiSettings = async () => {
  try {
    const settingsToSave = {
      page_size: pageSize.value,
      search_query: searchQuery.value,
      active_filters: {
        ...activeFilters.value,
        isDeleted: isMaintenanceMode.value ? activeFilters.value.isDeleted : '',
        excludeTargetSites: isCrossSeedMode.value ? activeFilters.value.excludeTargetSites : '',
      },
    }
    await axios.post('/api/ui_settings/cross_seed', settingsToSave)
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? (e.response?.data as { message?: string } | undefined)?.message || e.message
      : e instanceof Error
        ? e.message
        : String(e)
    console.error('无法保存UI设置:', message)
  }
}

const loadUiSettings = async () => {
  try {
    const response = await axios.get('/api/ui_settings/cross_seed')
    const settings = response.data
    pageSize.value = settings.page_size ?? 20
    searchQuery.value = settings.search_query ?? ''
    if (settings.active_filters) {
      activeFilters.value = {
        paths: Array.isArray(settings.active_filters.paths) ? settings.active_filters.paths : [],
        isDeleted: isMaintenanceMode.value ? String(settings.active_filters.isDeleted || '').trim() : '',
        excludeTargetSites: isCrossSeedMode.value
          ? String(settings.active_filters.excludeTargetSites || '').trim()
          : '',
      }
    }
  } catch (e) {
    console.error('加载UI设置时出错:', e)
  }
}

const loadReviewStatusFilter = async () => {
  try {
    const response = await axios.get('/api/config/cross_seed_review_filter')
    const result = response.data
    if (result.success) {
      reviewStatusFilter.value = result.data || ''
    }
  } catch (e) {
    console.error('加载检查状态筛选配置失败:', e)
  }
}

const handleSizeChange = (val: number) => {
  pageSize.value = val
  currentPage.value = 1
  void fetchData()
  void saveUiSettings()
}

const handleCurrentChange = (val: number) => {
  currentPage.value = val
  void fetchData()
}

const clearFilters = async () => {
  activeFilters.value = {
    paths: [],
    isDeleted: '',
    excludeTargetSites: '',
  }
  reviewStatusFilter.value = ''
  currentPage.value = 1
  try {
    await axios.post('/api/config/cross_seed_review_filter', { review_filter: '' })
  } catch (e) {
    console.error('保存检查状态筛选失败:', e)
  }
  void fetchData()
  void saveUiSettings()
}

const closeFilterDialog = () => {
  filterDialogVisible.value = false
  pendingBatchCrossSeedAfterFilter.value = false
}

const openFilterDialog = async () => {
  tempFilters.value = { ...activeFilters.value }
  filterDialogVisible.value = true

  if (!uniquePathsLoaded.value) {
    await loadUniquePaths()
  }

  await nextTick()

  if (pathTreeRef.value) {
    pathTreeRef.value.setCheckedKeys(activeFilters.value.paths, false)
  }
}

const applyFilters = () => {
  if (pathTreeRef.value) {
    tempFilters.value.paths = pathTreeRef.value.getCheckedKeys(false) as string[]
  }

  activeFilters.value = { ...tempFilters.value }
  filterDialogVisible.value = false
  const shouldContinueBatchCrossSeed =
    pendingBatchCrossSeedAfterFilter.value &&
    selectedRows.value.length > 0 &&
    activeFilters.value.excludeTargetSites.trim() !== ''
  pendingBatchCrossSeedAfterFilter.value = false
  currentPage.value = 1
  void fetchData()
  void saveUiSettings()

  if (shouldContinueBatchCrossSeed) {
    void handleBatchCrossSeed(activeFilters.value.excludeTargetSites.trim())
  }
}

watch(searchQuery, () => {
  if (uiInitializing.value) return
  if (searchDebounceTimer !== null) {
    window.clearTimeout(searchDebounceTimer)
  }
  searchDebounceTimer = window.setTimeout(() => {
    currentPage.value = 1
    void fetchData()
    void saveUiSettings()
    searchDebounceTimer = null
  }, 300)
})

const handleResize = () => {
  tableMaxHeight.value = window.innerHeight - 80
}

const tableRowClassName = ({ row }: { row: SeedParameter }) => {
  if (row.is_deleted || row.unrecognized) {
    return 'deleted-row'
  }
  if (!checkSelectable(row)) {
    return 'selected-row-disabled'
  }
  return ''
}

const handleMaintain = (row: SeedParameter) => {
  emit('maintain', row)
}

const handleSelectionChange = (selection: SeedParameter[]) => {
  selectedRows.value = selection
}

const buildBatchCrossSeedPayload = (rows: SeedParameter[], targetSiteName: string) => ({
  target_site_name: targetSiteName,
  seeds: rows.map((row) => ({
    hash: row.hash,
    torrent_id: row.torrent_id,
    site_name: row.site_name,
    nickname: row.nickname,
    downloader_id: row.downloader_id || '',
  })),
})

const routeToPublishLogs = async (queueGroupId?: string) => {
  await router.push({
    path: '/publish-logs',
    query: {
      ...(queueGroupId ? { queue_group_id: queueGroupId } : {}),
    },
  })
}

const enqueueCrossSeedRows = async (rows: SeedParameter[], targetSiteName: string) => {
  const response = await axios.post('/api/migrate/publish_queue/enqueue_batch', {
    ...buildBatchCrossSeedPayload(rows, targetSiteName),
    publish_scene: 'multi_torrent',
  })

  const result = response.data
  if (!result.success) {
    throw new Error(result.message || result.error || '批量入队失败')
  }

  const publishTrigger = String(result?.publish_trigger || '').trim()
  const groupID = String(result?.group_id || '').trim()
  const queued = Number(result?.queued || 0)
  const requested = Number(result?.requested || rows.length)

  ElMessage.success(
    `${result.message || `批量加入队列完成（${queued}/${requested}）`}${publishTrigger ? `（${publishTrigger}）` : ''}`,
  )

  await fetchData()

  if (publishTrigger) {
    await routeToPublishLogs(groupID || undefined)
  }
}

const handleBatchCrossSeedButtonClick = () => {
  if (selectedRows.value.length === 0) {
    ElMessage.warning('请先选择要批量转种的条目')
    return
  }

  const targetSiteName = activeFilters.value.excludeTargetSites.trim()
  if (!targetSiteName) {
    pendingBatchCrossSeedAfterFilter.value = true
    void openFilterDialog()
    return
  }

  void handleBatchCrossSeed()
}

const handleBatchCrossSeed = async (targetSiteOverride?: string) => {
  const targetSiteName = (targetSiteOverride || activeFilters.value.excludeTargetSites).trim()

  if (!targetSiteName) {
    ElMessage.warning('请先在筛选中选择目标站点')
    return
  }

  if (selectedRows.value.length === 0) {
    ElMessage.warning('请先选择要批量转种的条目')
    return
  }

  try {
    await enqueueCrossSeedRows(selectedRows.value, targetSiteName)
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

const handleSingleCrossSeed = async (row: SeedParameter) => {
  const targetSiteName = activeFilters.value.excludeTargetSites.trim()

  if (!targetSiteName) {
    ElMessage.warning('请先在筛选中选择目标站点')
    void openFilterDialog()
    return
  }

  if (!checkSelectable(row)) {
    ElMessage.warning('当前条目不可转种，请检查删除状态、检查状态或参数映射')
    return
  }

  try {
    await enqueueCrossSeedRows([row], targetSiteName)
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

const getDeleteButtonText = () => {
  if (!isDeleteMode.value) {
    return '批量删除模式'
  }
  if (selectedRows.value.length === 0) {
    return '退出删除模式'
  }
  return `删除选中项 (${selectedRows.value.length})`
}

const toggleDeleteMode = () => {
  isDeleteMode.value = !isDeleteMode.value
  selectedRows.value = []
}

const executeBatchDelete = async () => {
  if (selectedRows.value.length === 0) {
    ElMessage.warning('请先选择要删除的行')
    return
  }

  try {
    await ElMessageBox.confirm(
      `确定要删除选中的 ${selectedRows.value.length} 条种子数据吗？此操作无法恢复！`,
      '确认批量删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning',
      },
    )

    const deleteData = {
      items: selectedRows.value.map((row) => ({
        torrent_id: row.torrent_id,
        site_name: row.site_name,
      })),
    }

    const response = await axios.post('/api/cross-seed-data/delete', deleteData)
    const result = response.data

    if (!result.success) {
      throw new Error(result.error || '批量删除失败')
    }

    ElMessage.success(result.message || `成功删除 ${result.deleted_count} 条数据`)
    selectedRows.value = []
    isDeleteMode.value = false
    await fetchData()
  } catch (error: unknown) {
    if (error === 'cancel' || error === 'close') return
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

const openBatchFetchDialog = () => {
  batchFetchDialogVisible.value = true
}

const closeBatchFetchDialog = () => {
  batchFetchDialogVisible.value = false
}

const handleFetchCompleted = async () => {
  batchFetchDialogVisible.value = false
  ElMessage.success('批量获取种子数据已完成')
  await fetchData()
}

const openRecordViewDialog = () => {
  recordDialogVisible.value = true
}

const refreshCurrentView = async () => {
  if (isMaintenanceMode.value && batchFetchDialogVisible.value && batchFetchPanelRef.value) {
    await batchFetchPanelRef.value.refreshList()
    return
  }
  await fetchData()
}

onMounted(() => {
  emit('ready', refreshCurrentView)
})

onMounted(async () => {
  await Promise.all([
    loadUiSettings(),
    isMaintenanceMode.value ? loadReviewStatusFilter() : Promise.resolve(),
  ])
  uiInitializing.value = false
  void fetchData()
  window.addEventListener('resize', handleResize)
})

onUnmounted(() => {
  if (searchDebounceTimer !== null) {
    window.clearTimeout(searchDebounceTimer)
  }
  window.removeEventListener('resize', handleResize)
})
</script>

<style scoped lang="scss">
.modal-overlay,
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
  z-index: 2000;
}

.filter-card {
  width: 500px;
  max-width: 95vw;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}

.filter-card-header,
.modal-header {
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

.path-tree-container {
  max-height: 200px;
  overflow-y: auto;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 5px;
  margin-bottom: 20px;
}

:deep(.path-tree-node .el-tree-node__content) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.target-sites-container {
  margin-bottom: 20px;
}

.selected-site-display {
  margin-bottom: 10px;
}

.selected-site-info {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px;
}

.target-sites-radio-container {
  width: 100%;
  min-height: 100px;
  max-height: 200px;
  overflow-y: auto;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 10px;
  margin-bottom: 10px;
  box-sizing: border-box;
}

.target-sites-radio-group {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 8px;
  width: 100%;
}

:deep(.target-sites-radio-group .el-radio) {
  margin-right: 0 !important;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: flex;
  align-items: center;
}

:deep(.target-sites-radio-group .el-radio .el-radio__label) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.target-site-radio {
  margin-bottom: 8px;
}

.filter-card-footer {
  padding: 5px 10px;
  border-top: 1px solid var(--el-border-color-lighter);
  display: flex;
  justify-content: flex-end;
}

.batch-fetch-main-card {
  width: min(1100px, 100%);
  max-height: calc(100vh - 48px);
  display: flex;
  flex-direction: column;
}

.batch-fetch-main-content {
  min-height: 0;
  overflow: auto;
}

.cross-seed-data-view {
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

.table-container :deep(.el-table_2_column_23) {
  padding: 0;
}

.mapped-cell {
  text-align: center;
  line-height: 1.4;
}

.mapped-cell.invalid-value {
  color: #f56c6c;
  background-color: #fef0f0;
  font-weight: bold;
  padding: 8px 12px;
  height: calc(100% + 16px);
  display: flex;
  align-items: center;
  justify-content: center;
}

.datetime-cell {
  white-space: pre-line;
  line-height: 1.2;
}

:deep(.el-table_1_column_13) {
  padding: 0;
}

.tags-cell {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 2px;
  margin: -8px -12px;
  padding: 8px 12px;
  height: calc(100% + 16px);
  align-items: center;
}

.invalid-tag {
  background-color: #fef0f0 !important;
  border-color: #fbc4c4 !important;
}

.restricted-tag {
  background-color: #f56c6c !important;
  border-color: #f56c6c !important;
  color: #ffffff !important;
  font-weight: bold !important;
}

:deep(.deleted-row) {
  background-color: #fef0f0 !important;
  color: #f56c6c !important;
}

:deep(.deleted-row:hover) {
  background-color: #fde2e2 !important;
}

.title-cell {
  display: flex;
  flex-direction: column;
  justify-content: center;
  height: 100%;
  line-height: 1.4;
  text-align: left;
}

.subtitle-line,
.main-title-line {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  width: 100%;
}

.subtitle-line {
  font-size: 12px;
  margin-bottom: 2px;
}

.main-title-line {
  font-weight: 500;
}

:deep(
  .el-table__body
    tr.selected-row-disabled
    td.el-table-column--selection
    .cell
    .el-checkbox__input.is-disabled
    .el-checkbox__inner
) {
  border-color: #f56c6c !important;
  background-color: #fef0f0 !important;
}

:deep(
  .el-table__body
    tr.selected-row-disabled
    td.el-table-column--selection
    .cell
    .el-checkbox__input.is-disabled
    .el-checkbox__inner::after
) {
  border-color: #f56c6c !important;
}

.target-site-filter-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--el-color-danger);
  font-weight: 600;
}

@media (max-width: 768px) {
  .cross-seed-data-view {
    min-height: 0;
  }

  .search-and-controls {
    gap: 10px;
    padding: 10px;
  }

  .search-and-controls .search-input {
    width: 100% !important;
    margin-right: 0 !important;
  }

  .search-and-controls :deep(.el-radio-group) {
    width: 100%;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .target-sites-radio-group {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
