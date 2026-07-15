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

    <!-- 搜索和控制栏 -->
    <div class="search-and-controls glass-table">
      <el-input
        v-model="searchQuery"
        placeholder="搜索标题或种子ID..."
        clearable
        class="search-input"
        style="width: 300px; margin-right: 15px"
      />

      <!-- 批量转种按钮 -->
      <el-button
        type="success"
        @click="handleBatchCrossSeedButtonClick"
        plain
        style="margin-right: 15px"
        :disabled="isDeleteMode"
      >
        批量转种
      </el-button>

      <!-- 查看BDInfo记录按钮 -->
      <el-button type="info" @click="openRecordViewDialog" plain style="margin-right: 15px">
        BDInfo记录
      </el-button>

      <!-- 批量获取数据按钮 -->
      <el-button type="warning" @click="openBatchFetchDialog" plain style="margin-right: 15px">
        获取数据
      </el-button>

      <!-- 批量删除模式切换按钮 -->
      <el-button
        type="danger"
        @click="isDeleteMode && selectedRows.length > 0 ? executeBatchDelete() : toggleDeleteMode()"
        plain
        style="margin-right: 15px"
      >
        {{ getDeleteButtonText() }}
      </el-button>

      <!-- 筛选按钮 -->
      <el-button type="primary" @click="openFilterDialog" plain style="margin-right: 15px">
        筛选
      </el-button>

      <!-- 列显示/隐藏 -->
      <ColumnToggle
        v-model="visibleColumns"
        :columns="crossSeedColumns"
        :defaults="defaultVisibleColumns"
        style="margin-right: 15px"
      />

      <!-- 检查状态筛选单选组 -->
      <el-radio-group
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
          :page-sizes="[5, 10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
          background
        >
        </el-pagination>
      </div>
    </div>

    <!-- 筛选器弹窗 -->
    <div
      v-if="filterDialogVisible"
      class="filter-overlay"
      @click.self="filterDialogVisible = false"
    >
      <el-card class="filter-card">
        <template #header>
          <div class="filter-card-header">
            <span>筛选选项</span>
            <el-button type="danger" circle @click="filterDialogVisible = false" plain>X</el-button>
          </div>
        </template>
        <div class="filter-card-body">
          <el-divider content-position="left">保存路径</el-divider>
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
              :props="{ class: 'path-tree-node' }"
            />
          </div>

          <el-divider content-position="left">删除状态</el-divider>
          <el-radio-group v-model="tempFilters.isDeleted" style="width: 100%">
            <el-radio :label="''">全部</el-radio>
            <el-radio :label="'0'">未删除</el-radio>
            <el-radio :label="'1'">已删除</el-radio>
          </el-radio-group>

          <el-divider content-position="left">下载器</el-divider>
          <div style="margin-bottom: 10px">
            <div v-if="tempFilters.downloaderIds.length > 0" style="display: flex; align-items: center">
              <el-tag type="info" size="default" effect="plain">下载器: {{ tempFilters.downloaderIds.length }}</el-tag>
              <el-button type="danger" link style="padding: 0; margin-left: 5px" @click="clearDownloaderFilter">清除</el-button>
            </div>
          </div>
          <el-checkbox-group v-model="tempFilters.downloaderIds">
            <el-checkbox v-for="downloader in downloadersList" :key="downloader.id" :label="downloader.id">
              {{ downloader.name }}
            </el-checkbox>
          </el-checkbox-group>

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
        </div>
        <div class="filter-card-footer">
          <el-button @click="filterDialogVisible = false">取消</el-button>
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
        empty-text="暂无转种数据"
        :max-height="tableMaxHeight"
        height="100%"
        :row-class-name="tableRowClassName"
        @selection-change="handleSelectionChange"
        class="glass-table"
      >
        <el-table-column
          type="selection"
          width="55"
          align="center"
          :selectable="checkSelectable"
        ></el-table-column>
        <el-table-column
          v-if="isColumnVisible('torrent_id')"
          prop="torrent_id"
          label="种子ID"
          align="center"
          width="80"
          show-overflow-tooltip
        ></el-table-column>

        <el-table-column v-if="isColumnVisible('nickname')" prop="nickname" label="站点名称" width="100" align="center">
          <template #default="scope">
            <div class="mapped-cell">{{ scope.row.nickname }}</div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('downloader_id')" prop="downloader_id" label="下载器" width="100" align="center">
          <template #default="scope">
            <div class="mapped-cell">{{ getDownloaderName(scope.row.downloader_id) }}</div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('size')" prop="size" label="大小" width="100" align="center" sortable>
          <template #default="scope">
            <div class="mapped-cell">{{ formatBytes(scope.row.size) }}</div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('seeders')" prop="seed_site_count" label="做种站点数" width="100" align="center" sortable>
          <template #default="scope">
            <div class="mapped-cell">
              <el-tag
                :type="(scope.row.seed_site_count || 0) > 0 ? 'warning' : 'info'"
                size="small"
                class="clickable-tag"
                @click.stop="openSeedSitesDialog(scope.row)"
              >
                {{ scope.row.seed_site_count || 0 }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('title')" prop="title" label="标题" align="center">
          <template #default="scope">
            <div class="title-cell">
              <div class="subtitle-line" :title="scope.row.subtitle">
                {{ scope.row.subtitle || '' }}
              </div>
              <div
                class="main-title-line clickable-title"
                :title="scope.row.title"
                @click.stop="openSeedSitesDialog(scope.row)"
              >
                {{ scope.row.title || '' }}
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('type')" prop="type" label="类型" width="100" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.type) || !isMapped('type', scope.row.type),
              }"
            >
              {{ getMappedValue('type', scope.row.type) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('medium')" prop="medium" label="媒介" width="100" align="center">
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
        <el-table-column v-if="isColumnVisible('video_codec')" prop="video_codec" label="视频编码" width="120" align="center">
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
        <el-table-column v-if="isColumnVisible('audio_codec')" prop="audio_codec" label="音频编码" width="90" align="center">
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
        <el-table-column v-if="isColumnVisible('resolution')" prop="resolution" label="分辨率" width="90" align="center">
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
        <el-table-column v-if="isColumnVisible('team')" prop="team" label="制作组" width="120" align="center">
          <template #default="scope">
            <div
              class="mapped-cell"
              :class="{
                'invalid-value':
                  !isValidFormat(scope.row.team) || !isMapped('team', scope.row.team),
              }"
            >
              {{ getMappedValue('team', scope.row.team) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('source')" prop="source" label="产地" width="100" align="center">
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
          v-if="isColumnVisible('tags')"
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
        <el-table-column v-if="isColumnVisible('unrecognized')" prop="unrecognized" label="无法识别" width="120" align="center">
          <template #default="scope">
            <div class="mapped-cell" :class="{ 'invalid-value': scope.row.unrecognized }">
              {{ scope.row.unrecognized || '' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('publish_at')" prop="publish_at" label="可发种时间" width="170" align="center" sortable>
          <template #default="scope">
            <div v-if="editingPublishAtId === scope.row.id" class="publish-at-edit" @click.stop>
              <el-input-number
                v-model="editingPublishAtHours"
                :min="1"
                :step="1"
                size="small"
                style="width: 90px"
                controls-position="right"
                placeholder="小时"
                @keyup.enter="confirmPublishAt(scope.row)"
                ref="publishAtInputRef"
              />
              <span style="font-size: 12px; white-space: nowrap; margin-left: 2px; color: #909399">小时</span>
              <el-button
                type="danger"
                size="small"
                link
                style="margin-left: 4px"
                @click.stop="clearPublishAt(scope.row)"
                title="清除"
              >
                ✕
              </el-button>
            </div>
            <div
              v-else
              class="mapped-cell datetime-cell clickable-cell"
              @click.stop="startPublishAtEdit(scope.row)"
              :title="'点击设置可发种时间(小时)'"
            >
              {{ scope.row.publish_at ? formatDateTimeFull(scope.row.publish_at) : '未设置' }}
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="isColumnVisible('updated_at')" prop="updated_at" label="更新时间" width="140" align="center" sortable>
          <template #default="scope">
            <div class="mapped-cell datetime-cell">
              {{
                scope.row.is_deleted || hasRestrictedTag(scope.row.tags)
                  ? getRestrictionText(scope.row)
                  : formatDateTime(scope.row.updated_at)
              }}
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="130" align="center" fixed="right">
          <template #default="scope">
            <el-button size="small" type="primary" @click="handleEdit(scope.row)">编辑</el-button>
            <el-button
              size="small"
              type="danger"
              @click="handleDelete(scope.row)"
              style="margin-left: 5px"
              >删除</el-button
            >
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 转种弹窗 -->
    <div v-if="crossSeedDialogVisible" class="modal-overlay">
      <el-card class="cross-seed-card" shadow="always">
        <template #header>
          <div class="modal-header">
            <span>转种 - {{ selectedTorrentName }}</span>
            <el-button type="danger" circle @click="closeCrossSeedDialog" plain>X</el-button>
          </div>
        </template>
        <div class="cross-seed-content">
          <CrossSeedPanel
            :show-complete-button="true"
            publish-scene="multi_torrent"
            :prefetched-db-seed-info="prefetchedDbSeedInfo"
            @complete="handleCrossSeedComplete"
            @cancel="closeCrossSeedDialog"
          />
        </div>
      </el-card>
    </div>

    <!-- 处理记录查看弹窗 -->
    <BDInfoRecordsDialog v-model="recordDialogVisible" @closed="fetchData" />

    <!-- 批量获取数据弹窗 -->
    <div v-if="batchFetchDialogVisible" class="modal-overlay">
      <el-card class="batch-fetch-main-card" shadow="always">
        <template #header>
          <div class="modal-header">
            <span>批量获取种子数据</span>
            <el-button type="danger" circle @click="closeBatchFetchDialog" plain>X</el-button>
          </div>
        </template>
        <div class="batch-fetch-main-content">
          <BatchFetchPanel
            @cancel="closeBatchFetchDialog"
            @fetch-completed="handleFetchCompleted"
          />
        </div>
      </el-card>
    </div>

    <!-- 做种站点弹窗 -->
    <div v-if="seedSitesDialogVisible" class="modal-overlay" @click.self="closeSeedSitesDialog">
      <el-card class="seed-sites-card" shadow="always">
        <template #header>
          <div class="modal-header">
            <span class="seed-sites-title">{{ seedSitesTorrentName }}</span>
            <el-button type="danger" circle @click="closeSeedSitesDialog" plain>X</el-button>
          </div>
        </template>
        <div v-loading="seedSitesLoading" class="seed-sites-body">
          <div v-if="seedSitesData.length === 0 && !seedSitesLoading" class="seed-sites-empty">
            暂无做种站点信息
          </div>
          <div v-else class="seed-sites-grid">
            <div
              v-for="site in seedSitesData"
              :key="site.site_name"
              class="seed-site-card"
            >
              <div class="seed-site-name">{{ site.nickname || site.site_name }}</div>
            </div>
          </div>
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
import CrossSeedPanel from '../components/CrossSeedPanel.vue'
import BatchFetchPanel from '../components/BatchFetchPanel.vue'
import ColumnToggle from '../components/ColumnToggle.vue'
import type { ColumnDef } from '../components/ColumnToggle.vue'
import BDInfoRecordsDialog from '../components/cross-seed-data/BDInfoRecordsDialog.vue'
import { useCrossSeedStore } from '@/stores/crossSeed'
import { useTorrentsViewState } from '@/stores/torrentsViewState'
import type { Downloader } from '@/types'
import '@/assets/styles/glass-morphism.scss'
import { ElMessage } from '@/utils/uiNotify'

/**
 * Interface for the source site information used during cross-seeding.
 */
interface ISourceInfo {
  /**
   * The site's nickname, e.g., 'MTeam'.
   * This is used for display purposes.
   */
  name: string

  /**
   * The site's internal identifier, e.g., 'mteam'.
   * This is used for API calls.
   */
  site: string

  /**
   * The torrent ID on the source site.
   */
  torrentId: string
}

// 定义emit事件
const emit = defineEmits<{
  (e: 'ready', refreshMethod: () => Promise<void>): void
}>()

const router = useRouter()

// 在组件挂载时发送ready事件
onMounted(() => {
  emit('ready', fetchData)
})

interface SeedParameter {
  id: number
  hash: string
  torrent_id: string
  site_name: string
  nickname: string
  name: string
  downloader_id?: string
  size: number
  seeders: number
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
  is_reviewed: boolean // 新增：是否已检查
  publish_at: string | null // 可发种时间
}

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

// 反向映射表，用于将标准值映射到中文显示名称
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
const loading = ref<boolean>(true)
const error = ref<string | null>(null)

// 批量转种相关
const selectedRows = ref<SeedParameter[]>([])

// 批量获取数据相关
const batchFetchDialogVisible = ref<boolean>(false)

// 删除模式相关
const isDeleteMode = ref<boolean>(false)

// BDInfo记录查看相关
const recordDialogVisible = ref<boolean>(false)

// 路径树相关
const pathTreeRef = ref<InstanceType<typeof ElTree> | null>(null)
const pathTreeData = ref<PathNode[]>([])
const uniquePaths = ref<string[]>([])

// 表格高度
const tableMaxHeight = ref<number>(window.innerHeight - 80)

// 分页相关
const currentPage = ref<number>(1)
const pageSize = ref<number>(20)
const total = ref<number>(0)

// 搜索相关
const searchQuery = ref<string>('')

// 检查状态筛选
const reviewStatusFilter = ref<string>('')

// 处理检查状态筛选变化
const handleReviewStatusChange = async (value: string) => {
  reviewStatusFilter.value = value
  currentPage.value = 1
  // 调用后端API保存筛选状态到配置文件
  try {
    await axios.post('/api/config/cross_seed_review_filter', { review_filter: value })
  } catch (e) {
    console.error('保存检查状态筛选失败:', e)
  }
  fetchData()
}

// 计算当前筛选条件的显示文本
const currentFilterText = computed(() => {
  const filters = activeFilters.value
  const filterTexts = []

  // 处理保存路径筛选
  if (filters.paths && filters.paths.length > 0) {
    filterTexts.push(`路径: ${filters.paths.length}`)
  }

  // 处理删除状态筛选
  if (filters.isDeleted === '0') {
    filterTexts.push('未删除')
  } else if (filters.isDeleted === '1') {
    filterTexts.push('已删除')
  }

  // 处理不存在种子筛选
  if (filters.excludeTargetSites && filters.excludeTargetSites.trim() !== '') {
    filterTexts.push(`不存在于: ${filters.excludeTargetSites}`)
  }

  // 处理下载器筛选
  if (filters.downloaderIds && filters.downloaderIds.length > 0) {
    filterTexts.push(`下载器: ${filters.downloaderIds.length}`)
  }

  return filterTexts.join(', ')
})

// 检查是否有任何筛选条件被应用
const hasActiveFilters = computed(() => {
  const filters = activeFilters.value
  return (
    (filters.paths && filters.paths.length > 0) ||
    filters.isDeleted !== '' ||
    (filters.excludeTargetSites && filters.excludeTargetSites.trim() !== '') ||
    (filters.downloaderIds && filters.downloaderIds.length > 0)
  )
})

// 筛选相关
const filterDialogVisible = ref<boolean>(false)
const activeFilters = ref({
  paths: [] as string[], // 修改：改为数组类型
  isDeleted: '',
  excludeTargetSites: '', // 新增：排除目标站点筛选
  downloaderIds: [] as string[], // 下载器筛选
})
const tempFilters = ref({ ...activeFilters.value, downloaderIds: [...(activeFilters.value.downloaderIds || [])] })
const targetSitesList = ref<string[]>([]) // 新增：目标站点列表

// 下载器列表
const torrentsViewState = useTorrentsViewState()
const downloadersList = ref<Downloader[]>([])
const allDownloadersList = ref<Downloader[]>([])

const fetchDownloadersList = async (forceRefresh = false) => {
  const result = await torrentsViewState.fetchDownloadersList(forceRefresh)
  downloadersList.value = result.downloadersList
  allDownloadersList.value = result.allDownloadersList
}

// 根据下载器ID获取下载器名称
const getDownloaderName = (downloaderId: string | undefined) => {
  if (!downloaderId) return ''
  const downloader = allDownloadersList.value.find((d) => d.id === downloaderId)
  return downloader?.name || downloaderId
}

// 列定义与可见性
const crossSeedColumns: ColumnDef[] = [
  { prop: 'torrent_id', label: '种子ID' },
  { prop: 'nickname', label: '站点名称' },
  { prop: 'downloader_id', label: '下载器' },
  { prop: 'size', label: '大小' },
  { prop: 'seeders', label: '做种数' },
  { prop: 'title', label: '标题' },
  { prop: 'type', label: '类型' },
  { prop: 'medium', label: '媒介' },
  { prop: 'video_codec', label: '视频编码' },
  { prop: 'audio_codec', label: '音频编码' },
  { prop: 'resolution', label: '分辨率' },
  { prop: 'team', label: '制作组' },
  { prop: 'source', label: '产地' },
  { prop: 'tags', label: '标签' },
  { prop: 'unrecognized', label: '无法识别' },
  { prop: 'publish_at', label: '可发种时间' },
  { prop: 'updated_at', label: '更新时间' },
]

const defaultVisibleColumns = crossSeedColumns.map((c) => c.prop)
const visibleColumns = ref<string[]>([...defaultVisibleColumns])
const isColumnVisible = (prop: string) => visibleColumns.value.includes(prop)

// 计算属性：选中的目标站点（单选）
const selectedTargetSite = computed({
  get: () => {
    return tempFilters.value.excludeTargetSites || ''
  },
  set: (site) => {
    tempFilters.value.excludeTargetSites = site
  },
})

// 清除选中的目标站点
const clearSelectedTargetSite = () => {
  tempFilters.value.excludeTargetSites = ''
}

// 清除下载器筛选
const clearDownloaderFilter = () => {
  tempFilters.value.downloaderIds = []
}

// 辅助函数：获取映射后的中文值
const getMappedValue = (category: keyof ReverseMappings, standardValue: string) => {
  if (!standardValue) return ''

  const mappings = reverseMappings.value[category]
  if (!mappings) return standardValue

  return mappings[standardValue] || standardValue
}

// 检查值是否符合 *.* 格式
const isValidFormat = (value: string) => {
  if (!value) return true // 空值认为是有效的
  const regex = /^[^.]+[.][^.]+$/ // 匹配 *.* 格式
  return regex.test(value)
}

// 检查值是否已正确映射
const isMapped = (category: keyof ReverseMappings, standardValue: string) => {
  if (!standardValue) return true // 空值认为是有效的

  const mappings = reverseMappings.value[category]
  if (!mappings) return false // 没有映射表则认为未映射

  return !!mappings[standardValue] // 检查是否有对应的映射
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

const normalizeAndSortTags = (tags: string[] | string): string[] => {
  const tagList = normalizeTagList(tags)
    .map((tag) => String(tag || '').trim())
    .filter((tag) => tag)

  if (tagList.length <= 1) return tagList
  return [...tagList].sort(compareTagAZ)
}

const getTagsSortKey = (row: SeedParameter) => {
  return normalizeAndSortTags(row.tags).join('|').toLowerCase()
}

// 辅助函数：获取映射后的标签列表
const getMappedTags = (tags: string[] | string) => {
  const tagList = normalizeAndSortTags(tags)
  if (tagList.length === 0) {
    return []
  }

  // 映射标签到中文名称
  return tagList.map((tag: string) => {
    return reverseMappings.value.tags[tag] || tag
  })
}

// 获取标签的类型（用于显示不同颜色）
const getTagType = (tags: string[] | string, index: number) => {
  // 获取原始标签值
  const tagList = normalizeAndSortTags(tags)

  if (tagList.length === 0 || index >= tagList.length) return 'info'

  const originalTag = tagList[index]

  // 检查是否为禁转标签，如果是则显示为红色
  if (
    originalTag === '禁转' ||
    originalTag === 'tag.禁转' ||
    originalTag === '限转' ||
    originalTag === 'tag.限转' ||
    originalTag === '分集' ||
    originalTag === 'tag.分集'
  ) {
    return 'danger' // 红色
  }

  // 检查标签是否符合 *.* 格式且已映射
  if (!isValidFormat(originalTag) || !isMapped('tags', originalTag)) {
    return 'danger' // 红色
  }

  return 'info' // 默认蓝色
}

// 获取标签的自定义CSS类（用于背景色）
const getTagClass = (tags: string[] | string, index: number) => {
  // 获取原始标签值
  const tagList = normalizeAndSortTags(tags)

  if (tagList.length === 0 || index >= tagList.length) return ''

  const originalTag = tagList[index]

  // 检查是否为禁转标签
  if (
    originalTag === '禁转' ||
    originalTag === 'tag.禁转' ||
    originalTag === '限转' ||
    originalTag === 'tag.限转' ||
    originalTag === '分集' ||
    originalTag === 'tag.分集'
  ) {
    return 'restricted-tag' // 返回禁转标签的自定义类名
  }

  // 检查标签是否符合 *.* 格式且已映射
  if (!isValidFormat(originalTag) || !isMapped('tags', originalTag)) {
    return 'invalid-tag' // 返回自定义类名
  }

  return '' // 返回空字符串表示使用默认样式
}

// 格式化日期时间为完整的年月日时分秒格式，并支持换行显示
const formatDateTime = (dateString: string) => {
  if (!dateString) return ''

  try {
    const date = new Date(dateString)
    if (isNaN(date.getTime())) return dateString // 如果日期无效，返回原始字符串

    const year = date.getFullYear()
    const month = String(date.getMonth() + 1).padStart(2, '0')
    const day = String(date.getDate()).padStart(2, '0')
    const hours = String(date.getHours()).padStart(2, '0')
    const minutes = String(date.getMinutes()).padStart(2, '0')
    const seconds = String(date.getSeconds()).padStart(2, '0')
    return `${year}-${month}-${day}\n${hours}:${minutes}:${seconds}`
  } catch {
    return dateString // 如果解析失败，返回原始字符串
  }
}

// 格式化日期时间为单行完整格式（用于可发种时间列）
const formatDateTimeFull = (dateString: string) => {
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
    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
  } catch {
    return dateString
  }
}

// 可发种时间编辑状态（小时数输入）
const editingPublishAtId = ref<number | null>(null)
const editingPublishAtHours = ref<number>(24)
const publishAtInputRef = ref<InstanceType<any> | null>(null)

const startPublishAtEdit = (row: SeedParameter) => {
  editingPublishAtId.value = row.id
  editingPublishAtHours.value = 24
  nextTick(() => {
    if (publishAtInputRef.value) {
      const inner = publishAtInputRef.value.$el?.querySelector('input')
      inner?.focus()
      inner?.select()
    }
  })
}

const cancelPublishAtEdit = () => {
  editingPublishAtId.value = null
  editingPublishAtHours.value = 24
}

const computePublishAtFromHours = (hours: number): string => {
  const targetDate = new Date(Date.now() + hours * 60 * 60 * 1000)
  const year = targetDate.getFullYear()
  const month = String(targetDate.getMonth() + 1).padStart(2, '0')
  const day = String(targetDate.getDate()).padStart(2, '0')
  const h = String(targetDate.getHours()).padStart(2, '0')
  const m = String(targetDate.getMinutes()).padStart(2, '0')
  const s = String(targetDate.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day}T${h}:${m}:${s}`
}

const confirmPublishAt = async (row: SeedParameter) => {
  const hours = editingPublishAtHours.value
  if (!hours || hours <= 0) {
    ElMessage.warning('请输入有效的小时数')
    return
  }
  const publishAt = computePublishAtFromHours(hours)
  try {
    const response = await axios.post('/api/cross-seed-data/update_publish_at', {
      torrent_id: row.torrent_id,
      site_name: row.site_name,
      publish_at: publishAt,
    })
    const result = response.data
    if (result.success) {
      row.publish_at = publishAt
      ElMessage.success(`可发种时间已设置为 ${hours} 小时后`)
    } else {
      ElMessage.error(result.error || '更新失败')
    }
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { error?: string } | undefined)?.error || e.message)
      : e instanceof Error
        ? e.message
        : '网络错误'
    ElMessage.error(message)
  } finally {
    editingPublishAtId.value = null
    editingPublishAtHours.value = 24
  }
}

const handlePublishAtChange = confirmPublishAt

const clearPublishAt = async (row: SeedParameter) => {
  try {
    const response = await axios.post('/api/cross-seed-data/update_publish_at', {
      torrent_id: row.torrent_id,
      site_name: row.site_name,
      publish_at: null,
    })
    const result = response.data
    if (result.success) {
      row.publish_at = null
      ElMessage.success('已清除可发种时间')
    } else {
      ElMessage.error(result.error || '清除失败')
    }
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { error?: string } | undefined)?.error || e.message)
      : e instanceof Error
        ? e.message
        : '网络错误'
    ElMessage.error(message)
  } finally {
    editingPublishAtId.value = null
    editingPublishAtHours.value = 24
  }
}

// 格式化文件大小
const formatBytes = (bytes: number | null | undefined): string => {
  if (bytes == null || bytes <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(2)} ${units[i]}`
}

// 做种站点弹窗相关
interface SeedSiteInfo {
  site_name: string
  nickname: string
}

const seedSitesDialogVisible = ref(false)
const seedSitesData = ref<SeedSiteInfo[]>([])
const seedSitesLoading = ref(false)
const seedSitesTorrentName = ref('')

const openSeedSitesDialog = async (row: SeedParameter) => {
  if (!row.name) return
  seedSitesTorrentName.value = row.title || row.name
  seedSitesDialogVisible.value = true
  seedSitesLoading.value = true
  seedSitesData.value = []
  try {
    const response = await axios.get('/api/cross-seed-data/seed-sites', {
      params: { name: row.name },
    })
    const result = response.data
    if (result.success && Array.isArray(result.sites)) {
      seedSitesData.value = result.sites.map((s: Record<string, unknown>) => ({
        site_name: String(s.site_name || ''),
        nickname: String(s.nickname || ''),
      }))
    }
  } catch (e) {
    console.error('获取做种站点信息失败:', e)
  } finally {
    seedSitesLoading.value = false
  }
}

const closeSeedSitesDialog = () => {
  seedSitesDialogVisible.value = false
  seedSitesData.value = []
}

// 检查行是否有无效参数
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

  let tagList: string[] = []
  if (typeof row.tags === 'string') {
    try {
      tagList = JSON.parse(row.tags)
    } catch {
      tagList = row.tags
        .split(',')
        .map((tag) => tag.trim())
        .filter((tag) => tag)
    }
  } else if (Array.isArray(row.tags)) {
    tagList = row.tags
  }

  for (const tag of tagList) {
    if (!isValidFormat(tag) || !isMapped('tags', tag)) {
      return true
    }
  }

  if (row.unrecognized) {
    return true
  }

  return false
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

const fetchData = async () => {
  loading.value = true
  error.value = null
  try {
    const params = new URLSearchParams({
      page: currentPage.value.toString(),
      page_size: pageSize.value.toString(),
      search: searchQuery.value,
      path_filters: JSON.stringify(activeFilters.value.paths || []),
      downloader_filters: JSON.stringify(activeFilters.value.downloaderIds || []),
      is_deleted: activeFilters.value.isDeleted,
      exclude_target_sites: activeFilters.value.excludeTargetSites,
      review_status: reviewStatusFilter.value, // 新增：检查状态筛选参数
    })

    // 调试日志：检查筛选参数
    if (activeFilters.value.excludeTargetSites) {
      console.log('发送目标站点排除参数:', activeFilters.value.excludeTargetSites)
    }

    const response = await axios.get(`/api/cross-seed-data?${params.toString()}`)
    const result = response.data

    if (result.success) {
      tableData.value = result.data
      total.value = result.total

      // 更新反向映射表
      if (result.reverse_mappings) {
        reverseMappings.value = result.reverse_mappings
      }

      // 更新唯一路径数据并构建路径树
      if (result.unique_paths) {
        uniquePaths.value = result.unique_paths
        pathTreeData.value = buildPathTree(result.unique_paths)
      }

      // 更新目标站点列表
      if (result.target_sites) {
        const filteredTargetSites = (result.target_sites || []).filter((site: string) => {
          const normalized = String(site || '')
            .trim()
            .toLowerCase()

          if (normalized !== 'ilolicon') {
            return true
          }

          return result.data.some((row: SeedParameter) => isAnimationRelatedType(row.type))
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
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (e.response?.data as { error?: string } | undefined)?.error ||
        e.message)
      : e instanceof Error
        ? e.message
        : '网络错误'
    error.value = message
    ElMessage.error(message)
  } finally {
    loading.value = false
  }
}

const saveUiSettings = async () => {
  try {
    const settingsToSave = {
      page_size: pageSize.value,
      search_query: searchQuery.value,
      active_filters: activeFilters.value,
      visible_columns: visibleColumns.value,
    }
    await axios.post('/api/ui_settings/cross_seed', settingsToSave)
  } catch (e: unknown) {
    const message = axios.isAxiosError(e)
      ? ((e.response?.data as { message?: string } | undefined)?.message || e.message)
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
      Object.assign(activeFilters.value, settings.active_filters)
    }
    if (Array.isArray(settings.visible_columns)) {
      visibleColumns.value = settings.visible_columns
    }
  } catch (e) {
    console.error('加载UI设置时出错:', e)
  }
}

const handleSizeChange = (val: number) => {
  pageSize.value = val
  currentPage.value = 1
  fetchData()
  saveUiSettings()
}

const handleCurrentChange = (val: number) => {
  currentPage.value = val
  fetchData()
}

// 清除筛选条件
const clearFilters = () => {
  activeFilters.value = {
    paths: [],
    isDeleted: '',
    excludeTargetSites: '', // 新增：清除目标站点排除筛选
    downloaderIds: [],
  }
  currentPage.value = 1
  fetchData()
  saveUiSettings()
}

// 打开筛选对话框
const openFilterDialog = () => {
  // 将当前活动的筛选条件复制到临时筛选条件
  tempFilters.value = {
    ...activeFilters.value,
    downloaderIds: [...(activeFilters.value.downloaderIds || [])],
  }
  filterDialogVisible.value = true
  nextTick(() => {
    // 如果已有选中的路径，设置树的选中状态
    if (pathTreeRef.value && activeFilters.value.paths.length > 0) {
      // 设置树的选中状态
      pathTreeRef.value.setCheckedKeys(activeFilters.value.paths, false)
    }
  })
}

// 应用筛选条件
const applyFilters = () => {
  // 从路径树中获取选中的路径
  if (pathTreeRef.value) {
    const selectedPaths = pathTreeRef.value.getCheckedKeys(false) as string[]
    tempFilters.value.paths = selectedPaths
  }

  // 将临时筛选条件应用为活动筛选条件
  activeFilters.value = {
    ...tempFilters.value,
    downloaderIds: [...(tempFilters.value.downloaderIds || [])],
  }
  filterDialogVisible.value = false
  // 重置到第一页并获取数据
  currentPage.value = 1
  fetchData()
  saveUiSettings()
}

const crossSeedStore = useCrossSeedStore()
const prefetchedDbSeedInfo = ref<Record<string, unknown> | undefined>(undefined)
const uiInitializing = ref(true)

// 监听搜索查询的变化，自动触发搜索
watch(searchQuery, () => {
  if (uiInitializing.value) return
  currentPage.value = 1
  fetchData()
  saveUiSettings()
})

watch(visibleColumns, () => {
  if (uiInitializing.value) return
  saveUiSettings()
})

// 控制转种弹窗的显示
const crossSeedDialogVisible = computed(() => !!crossSeedStore.taskId)
const selectedTorrentName = computed(() => {
  const params = crossSeedStore.workingParams as { title?: string; name?: string } | null
  return params?.title ?? params?.name ?? ''
})

// 处理编辑按钮点击
const handleEdit = async (row: SeedParameter) => {
  try {
    // 重置 store
    crossSeedStore.reset()
    prefetchedDbSeedInfo.value = undefined

    // 从后端API获取详细的种子参数
    const response = await axios.get(
      `/api/migrate/get_db_seed_info?torrent_id=${row.torrent_id}&site_name=${row.site_name}`,
    )
    const result = response.data

    if (result.success) {
      prefetchedDbSeedInfo.value = result
      // 将获取到的数据设置到 store 中
      // 构造一个基本的 Torrent 对象结构
      const torrentData = {
        ...result.data,
        // 优先使用数据库中的name列，如果不存在则使用title列
        name: result.data.name || result.data.title,
        // 使用从数据库获取的实际保存路径，如果没有则为空字符串
        save_path: result.data.save_path || '',
        size: 0,
        size_formatted: '0 B',
        progress: 100,
        state: 'completed',
        total_uploaded: 0,
        total_uploaded_formatted: '0 B',
        // 添加下载器ID（如果从数据库返回了）
        downloaderId: result.data.downloader_id || null,
        sites: {
          [result.data.site_name]: {
            torrentId: result.data.torrent_id,
            comment: `id=${result.data.torrent_id}`, // 为了向后兼容，也提供comment格式
          },
        },
      }

      crossSeedStore.setParams(torrentData)

      // 设置源站点信息
      const sourceInfo: ISourceInfo = {
        name: result.data.site_name,
        site: result.data.site_name.toLowerCase(), // 假设站点标识符是站点名称的小写形式
        torrentId: result.data.torrent_id,
      }
      crossSeedStore.setSourceInfo(sourceInfo)

      // 设置一个任务ID以显示弹窗
      crossSeedStore.setTaskId(`cross_seed_${row.id}_${Date.now()}`)
    } else {
      ElMessage.error(result.error || '获取种子参数失败')
    }
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message)
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

// 处理删除按钮点击
const handleDelete = async (row: SeedParameter) => {
  try {
    // 确认是否删除
    await ElMessageBox.confirm(
      `确定要永久删除种子数据 "${row.title}" 吗？此操作无法恢复！`,
      '确认永久删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning',
      },
    )

    // 向后端发送删除请求 - 使用统一的 delete API
    const deleteData = {
      torrent_id: row.torrent_id,
      site_name: row.site_name,
    }
    const response = await axios.post('/api/cross-seed-data/delete', deleteData)

    const result = response.data

    if (result.success) {
      ElMessage.success(result.message || `删除成功`)
      // 重新获取数据，以更新表格
      fetchData()
    } else {
      ElMessage.error(result.error || '删除失败')
    }
  } catch (error: unknown) {
    if (error === 'cancel' || error === 'close') return
    const message = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message)
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

// 关闭转种弹窗
const closeCrossSeedDialog = () => {
  prefetchedDbSeedInfo.value = undefined
  crossSeedStore.reset()
}

// 处理转种完成
const handleCrossSeedComplete = () => {
  ElMessage.success('转种操作已完成！')
  prefetchedDbSeedInfo.value = undefined
  crossSeedStore.reset()
  // 可选：刷新数据以显示最新状态
  fetchData()
}

// 处理窗口大小变化
const handleResize = () => {
  tableMaxHeight.value = window.innerHeight - 80
}

onMounted(async () => {
  // 加载UI设置
  await loadUiSettings()
  // 加载检查状态筛选配置
  await loadReviewStatusFilter()
  // 加载下载器列表
  await fetchDownloadersList()
  uiInitializing.value = false
  // 获取数据
  fetchData()
  window.addEventListener('resize', handleResize)
})

// 加载检查状态筛选配置
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

// 为表格行设置CSS类名
const tableRowClassName = ({ row }: { row: SeedParameter }) => {
  // 红色背景：已删除、包含禁转标签、或有无法识别的内容
  if (row.is_deleted || hasRestrictedTag(row.tags) || row.unrecognized) {
    return 'deleted-row'
  }
  // 如果未检查，添加unreviewed-row类（蓝色背景）
  if (!row.is_reviewed) {
    return 'unreviewed-row'
  }
  // 如果行不可选择，添加selected-row-disabled类
  if (!checkSelectable(row)) {
    return 'selected-row-disabled'
  }
  return ''
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

// 检查标签中是否包含禁转标签
const hasRestrictedTag = (tags: string[] | string): boolean => {
  const tagList = normalizeTagList(tags)

  // 检查是否包含"禁转"或"tag.禁转"
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
  const tagList = normalizeTagList(row.tags)

  if (row.is_deleted) {
    labels.push('已删除做种文件')
  }

  const restrictedTags: string[] = []
  if (tagList.some((tag) => tag === '禁转' || tag === 'tag.禁转')) {
    restrictedTags.push('禁转')
  }
  if (tagList.some((tag) => tag === '限转' || tag === 'tag.限转')) {
    restrictedTags.push('限转')
  }
  if (tagList.some((tag) => tag === '分集' || tag === 'tag.分集')) {
    restrictedTags.push('分集')
  }
  if (restrictedTags.length > 0) {
    labels.push(restrictedTags.join('/'))
  }

  return labels.join('\n')
}

// 控制表格行是否可选择
const checkSelectable = (row: SeedParameter) => {
  // 在删除模式下，所有行都可以被选择（包括已删除的行和有禁转标签的行）
  if (isDeleteMode.value) {
    return true
  }

  // 在非删除模式下，检查是否包含禁转标签
  if (hasRestrictedTag(row.tags)) {
    return false
  }

  // 如果已删除筛选处于活动状态，则允许选择已删除的行 - 便于批量操作
  if (activeFilters.value.isDeleted === '1') {
    // 但仍需检查是否有无效参数
    return !hasInvalidParams(row)
  } else {
    // 在正常模式下，已删除的行不可选择；有无效参数的行也不可选择
    if (row.is_deleted) {
      return false
    }
    // 如果有无效参数，则不可选择
    if (hasInvalidParams(row)) {
      return false
    }
    // 如果未检查（is_reviewed 为 false 或 0），则不可选择
    if (!row.is_reviewed) {
      return false
    }
    return true
  }
}

// 处理表格选中行变化
const handleSelectionChange = (selection: SeedParameter[]) => {
  selectedRows.value = selection

  // 在删除模式下，根据选择状态更新按钮文字
  if (isDeleteMode.value) {
    // 这里会通过计算属性自动更新按钮文字
  }
}

// 处理批量转种按钮点击
const handleBatchCrossSeedButtonClick = () => {
  const targetSiteName = activeFilters.value.excludeTargetSites.trim()

  if (!targetSiteName || selectedRows.value.length === 0) {
    openFilterDialog()
    return
  }

  void handleBatchCrossSeed()
}

// 处理批量转种
const handleBatchCrossSeed = async () => {
  // 直接使用筛选中的站点
  const targetSiteName = activeFilters.value.excludeTargetSites

  if (!targetSiteName || targetSiteName.trim() === '') {
    ElMessage.warning('请先在筛选中选择目标站点')
    return
  }

  try {
    // 1. 构造要传递给后端的数据
    const batchData = {
      target_site_name: targetSiteName,
      seeds: selectedRows.value.map((row) => ({
        hash: row.hash,
        torrent_id: row.torrent_id,
        site_name: row.site_name,
        nickname: row.nickname,
        downloader_id: row.downloader_id || '',
      })),
    }

    console.log('批量转种数据:', batchData)

    // 2. 调用批量入队接口
    const response = await axios.post('/api/migrate/publish_queue/enqueue_batch', {
      ...batchData,
      publish_scene: 'multi_torrent',
    })

    const result = response.data
    if (result.success) {
      const publishTrigger = String(result?.publish_trigger || '').trim()
      const groupID = String(result?.group_id || '').trim()
      const queued = Number(result?.queued || 0)
      const requested = Number(result?.requested || selectedRows.value.length)
      ElMessage.success(
        `${result.message || `批量加入队列完成（${queued}/${requested}）`}${publishTrigger ? `（${publishTrigger}）` : ''}`,
      )
      if (publishTrigger) {
        await router.push({
          path: '/publish-logs',
          query: {
            trigger: publishTrigger,
            scene: 'multi_torrent',
            ...(groupID ? { queue_group_id: groupID } : {}),
          },
        })
      }
    } else {
      ElMessage.error(result.message || result.error || '批量入队失败')
    }
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message)
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

// 获取删除按钮文字
const getDeleteButtonText = () => {
  if (!isDeleteMode.value) {
    return '批量删除模式'
  }

  if (selectedRows.value.length === 0) {
    return '退出删除模式'
  }

  return `删除选中项 (${selectedRows.value.length})`
}

// 切换删除模式
const toggleDeleteMode = async () => {
  if (isDeleteMode.value) {
    // 当前处于删除模式，退出删除模式
    isDeleteMode.value = false
    // 清空选中行
    selectedRows.value = []
  } else {
    // 进入删除模式
    isDeleteMode.value = true
    // 清空之前的选择
    selectedRows.value = []
  }
}

// 执行批量删除
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

    if (result.success) {
      ElMessage.success(result.message || `成功删除 ${result.deleted_count} 条数据`)
      // 清空选中行并退出删除模式
      selectedRows.value = []
      isDeleteMode.value = false
      // 重新获取数据
      fetchData()
    } else {
      ElMessage.error(result.error || '批量删除失败')
    }
  } catch (error: unknown) {
    if (error === 'cancel' || error === 'close') return
    const message = axios.isAxiosError(error)
      ? ((error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message)
      : error instanceof Error
        ? error.message
        : '网络错误'
    ElMessage.error(message)
  }
}

// 打开批量获取数据对话框
const openBatchFetchDialog = () => {
  batchFetchDialogVisible.value = true
}

// 关闭批量获取数据对话框
const closeBatchFetchDialog = () => {
  batchFetchDialogVisible.value = false
}

// 处理批量获取完成事件
const handleFetchCompleted = () => {
  ElMessage.success('批量获取种子数据已完成，正在刷新列表...')
  // 刷新种子列表
  fetchData()
}

// 打开记录查看对话框
const openRecordViewDialog = () => {
  recordDialogVisible.value = true
}

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
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

.cross-seed-card {
  width: 90vw;
  max-width: 1200px;
  height: 90vh;
  display: flex;
  flex-direction: column;
}

:deep(.cross-seed-card .el-card__body) {
  padding: 10px;
  flex: 1;
  display: flex;
  flex-direction: column;
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.cross-seed-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
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

.clickable-cell {
  cursor: pointer;
  transition: background-color 0.2s;
}

.clickable-cell:hover {
  background-color: #f0f7ff;
  color: #409eff;
}

.publish-at-edit {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 2px;
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

/* 未检查行的样式（黄色背景） */
:deep(.unreviewed-row) {
  background-color: #aadbf3 !important;
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

/* 不可选择行的复选框变红 */
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

/* 批量获取数据弹窗样式 */
.batch-fetch-main-card {
  width: 95vw;
  max-width: 1400px;
  height: 85vh;
  max-height: 900px;
  display: flex;
  flex-direction: column;
}

:deep(.batch-fetch-main-card .el-card__body) {
  padding: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.batch-fetch-main-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
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

.clickable-title {
  cursor: pointer;
  color: #409eff;
}

.clickable-title:hover {
  text-decoration: underline;
  color: #337ecc;
}

.clickable-tag {
  cursor: pointer;
}

.clickable-tag:hover {
  opacity: 0.8;
}

/* 做种站点弹窗 */
.seed-sites-card {
  width: 600px;
  max-width: 90vw;
  max-height: 70vh;
  display: flex;
  flex-direction: column;
}

.seed-sites-title {
  font-weight: 600;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: calc(100% - 40px);
}

.seed-sites-body {
  min-height: 80px;
  max-height: 50vh;
  overflow-y: auto;
}

.seed-sites-empty {
  text-align: center;
  color: #909399;
  padding: 30px 0;
  font-size: 14px;
}

.seed-sites-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 12px;
  padding: 8px 0;
}

.seed-site-card {
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  padding: 10px;
  background: #fafbfc;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  transition: all 0.2s ease;
}

.seed-site-card:hover {
  border-color: #b3d8ff;
  box-shadow: 0 2px 8px rgba(179, 216, 255, 0.2);
  transform: translateY(-1px);
}

.seed-site-name {
  font-weight: 600;
  font-size: 13px;
  color: #303133;
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}
</style>
