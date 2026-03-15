<template>
  <div class="cross-seed-panel">
    <CrossSeedStepsHeader :steps="steps" :active-step="activeStep" />

    <!-- 2. 中间内容区 -->
    <main class="panel-content">
      <!-- 步骤 0: 核对种子详情 -->
      <CrossSeedStepDetails v-if="activeStep === 0" />

      <!-- 步骤 1: 发布参数预览 -->
      <CrossSeedStepPublishPreview v-if="activeStep === 1" />

      <!-- 步骤 2: 选择发布站点 -->
      <CrossSeedStepSiteSelection v-if="activeStep === 2" />

      <!-- 步骤 3: 完成发布 -->
      <CrossSeedStepPublishResults v-if="activeStep === 3" />
    </main>

    <!-- 3. 底部按钮栏 (固定) -->
    <CrossSeedPanelFooter />
  </div>

  <!-- 日志弹窗 (保持不变) -->
  <LogViewerCard v-model="showLogCard" title="操作日志" :content="logContent" />

  <!-- 日志进度组件 -->
  <LogProgress
    :visible="showLogProgress"
    :taskId="logProgressTaskId"
    :error-message="fetchFlowErrorMessage"
    @complete="handleLogProgressComplete"
    @close="handleFetchLogProgressClose"
  />

  <el-dialog
    v-model="showScreenshotPreviewDialog"
    title="选择截图候选"
    width="1200px"
    class="screenshot-preview-dialog"
    destroy-on-close
    @closed="resetScreenshotPreviewState"
  >
    <div class="screenshot-preview-dialog__hint">
      只有在未找到字幕流时，才会生成低清候选图供手动选择；点击左侧缩略图可在右侧放大查看。
    </div>
    <div class="screenshot-preview-dialog__toolbar">
      <span
        >已选择 {{ selectedScreenshotPreviewIds.length }}/{{
          screenshotPreviewSelectionLimit
        }}
        张</span
      >
      <span class="screenshot-preview-dialog__toolbar-tip">左侧滚动浏览，右侧查看大图</span>
    </div>
    <div class="screenshot-preview-dialog__layout">
      <div class="screenshot-preview-dialog__sidebar">
        <button
          v-for="candidate in screenshotPreviewCandidates"
          :key="candidate.id"
          type="button"
          class="screenshot-preview-dialog__item"
          :class="{
            'is-active': activeScreenshotPreviewId === candidate.id,
            'is-selected': isScreenshotPreviewSelected(candidate.id),
          }"
          @click="setActiveScreenshotPreview(candidate.id)"
        >
          <img
            :src="candidate.previewData"
            :alt="candidate.timeLabel"
            class="screenshot-preview-dialog__item-thumb"
          />
          <div class="screenshot-preview-dialog__item-meta">
            <span>{{ candidate.timeLabel }}</span>
            <el-checkbox
              :model-value="isScreenshotPreviewSelected(candidate.id)"
              @click.stop
              @change="() => toggleScreenshotPreviewSelection(candidate.id)"
            >
              选择
            </el-checkbox>
          </div>
        </button>
      </div>
      <div class="screenshot-preview-dialog__viewer">
        <template v-if="activeScreenshotPreviewCandidate">
          <div class="screenshot-preview-dialog__viewer-header">
            <span>{{ activeScreenshotPreviewCandidate.timeLabel }}</span>
            <el-button
              size="small"
              :type="
                isScreenshotPreviewSelected(activeScreenshotPreviewCandidate.id)
                  ? 'primary'
                  : 'default'
              "
              @click="toggleScreenshotPreviewSelection(activeScreenshotPreviewCandidate.id)"
            >
              {{
                isScreenshotPreviewSelected(activeScreenshotPreviewCandidate.id)
                  ? '取消选择'
                  : '选择此张'
              }}
            </el-button>
          </div>
          <div class="screenshot-preview-dialog__viewer-image-wrap">
            <img
              :src="activeScreenshotPreviewCandidate.previewData"
              :alt="activeScreenshotPreviewCandidate.timeLabel"
              class="screenshot-preview-dialog__viewer-image"
            />
          </div>
        </template>
      </div>
    </div>
    <template #footer>
      <div class="screenshot-preview-dialog__footer">
        <el-button
          @click="showScreenshotPreviewDialog = false"
          :disabled="isFinalizingScreenshotPreview"
        >
          取消
        </el-button>
        <el-button
          type="primary"
          @click="confirmScreenshotPreviewSelection"
          :loading="isFinalizingScreenshotPreview"
          :disabled="selectedScreenshotPreviewIds.length !== screenshotPreviewSelectionLimit"
        >
          生成正式截图
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
// ... 你的 <script setup> 部分完全保持不变 ...
import { ref, onMounted, onUnmounted, computed, nextTick, watch, provide } from 'vue'
import axios from 'axios'
import { useCrossSeedStore } from '@/stores/crossSeed'
import { ElNotification } from '@/utils/uiNotify'
import { openSSE, type EventSourceLike } from '@/desktop/sse'
import LogProgress from './LogProgress.vue'
import LogViewerCard from './LogViewerCard.vue'
import CrossSeedStepsHeader from './cross-seed/CrossSeedStepsHeader.vue'
import CrossSeedStepDetails from './cross-seed/CrossSeedStepDetails.vue'
import CrossSeedStepPublishPreview from './cross-seed/CrossSeedStepPublishPreview.vue'
import CrossSeedStepSiteSelection from './cross-seed/CrossSeedStepSiteSelection.vue'
import CrossSeedStepPublishResults from './cross-seed/CrossSeedStepPublishResults.vue'
import CrossSeedPanelFooter from './cross-seed/CrossSeedPanelFooter.vue'
import './cross-seed/crossSeedPanel.scss'
import { createPublishFlow, type RawPublishResult } from './cross-seed/panel/publishFlow'
import { createSeedFlow } from './cross-seed/panel/seedFlow'
import type { WorkingTorrent } from './cross-seed/panel/types'
import { crossSeedPanelContextKey } from './cross-seed/crossSeedPanelContext'
import type {
  BdinfoProgress,
  CrossSeedPanelContext,
  LimitAlert,
  ProgressCounter,
  ReverseMappings,
  ScreenshotReviewStatus,
  StandardizedParams,
  TitleComponent,
  TorrentData,
} from './cross-seed/crossSeedPanelContext'

// 过滤多余空行的辅助函数
const filterExtraEmptyLines = (text: string): string => {
  if (!text) return ''
  // 过滤掉多余的空行，保留项目间的单个空行
  // 先去除行尾空格和其他空白字符
  text = text.replace(/[ \t\f\v]+$/gm, '')
  // 去除开头和结尾的空行
  text = text.replace(/^\s*\n+/, '').replace(/\n\s*$/, '')
  // 将两个或更多连续的空行替换为单个换行符（即一个空行）
  text = text.replace(/(\n\s*){2,}/g, '\n\n')
  // 处理句子和列表之间的多余空行（更通用的处理方式）
  text = text.replace(/([^\n]+。)\s*\n\s*\n(\s*\d+\.)/g, '$1\n$2')
  // 处理列表项之间的多余空行
  text = text.replace(/(\d+\.[\s\S]*?)\n\s*\n(\s*\d+\.)/g, '$1\n$2')
  // 处理嵌套标签内的多余空行（例如[b][color]标签内的空行）
  text = text.replace(
    /(\[(?:b|color)[^\]]*\][\s\S]*?)\n\s*\n([\s\S]*?\[\/(?:b|color)\])/gi,
    '$1\n$2',
  )
  // 处理多层嵌套标签
  for (let i = 0; i < 3; i++) {
    text = text.replace(
      /(\[(?:quote|b|color|size)[^\]]*\][\s\S]*?)\n\s*\n([\s\S]*?\[\/(?:quote|b|color|size)\])/gi,
      '$1\n$2',
    )
  }
  // 再次处理可能仍然存在的多余空行
  text = text.replace(/(\n\s*){2,}/g, '\n\n')
  return text
}

const splitMediaInfoFromIntroBody = (
  rawBody: string,
): { body: string; extractedMediainfo: string } => {
  const body = (rawBody || '').replace(/\r\n/g, '\n')
  if (!body.trim()) return { body: '', extractedMediainfo: '' }

  const markerRe = /\[url=javascript:void\(0\)\]\s*mediainfo\s*-\s*[\s\S]*?\[\/url\]\s*/i
  const markerMatch = markerRe.exec(body)
  if (markerMatch && typeof markerMatch.index === 'number') {
    const extracted = body
      .slice(markerMatch.index)
      .replace(markerRe, '')
      .replace(/\u00a0/g, ' ')
      .trim()
    return {
      body: body.slice(0, markerMatch.index).trimEnd(),
      extractedMediainfo: extracted,
    }
  }

  const generalIndex = body.search(/(?:^|\n)General\s*\n/i)
  if (generalIndex < 0) return { body: rawBody || '', extractedMediainfo: '' }

  const tail = body.slice(generalIndex)
  const looksLikeMediaInfo = /Overall\s*bit\s*rate/i.test(tail) && /Complete\s*name/i.test(tail)
  if (!looksLikeMediaInfo) return { body: rawBody || '', extractedMediainfo: '' }

  return {
    body: body.slice(0, generalIndex).trimEnd(),
    extractedMediainfo: tail.replace(/\u00a0/g, ' ').trim(),
  }
}

const normalizeIntroBodyAndMediainfo = (
  rawBody: string,
  rawMediainfo: string,
): { body: string; mediainfo: string } => {
  const cleanedBody = filterExtraEmptyLines(rawBody)
  const { body, extractedMediainfo } = splitMediaInfoFromIntroBody(cleanedBody)
  const mediainfo = (rawMediainfo || '').replace(/\u00a0/g, ' ').trim()
  return {
    body: filterExtraEmptyLines(body),
    mediainfo: mediainfo || extractedMediainfo,
  }
}

// BBCode 解析函数
const parseBBCode = (text?: string | null): string => {
  if (!text) return ''

  // 过滤掉多余的空行，只保留单个空行
  text = filterExtraEmptyLines(text)

  // 处理 [quote] 标签
  text = text.replace(/\[quote\]([\s\S]*?)\[\/quote\]/gi, '<blockquote>$1</blockquote>')

  // 处理 [b] 标签
  text = text.replace(/\[b\]([\s\S]*?)\[\/b\]/gi, '<strong>$1</strong>')

  // 处理 [color] 标签
  text = text.replace(
    /\[color=(\w+|#[0-9a-fA-F]{3,6})\]([\s\S]*?)\[\/color\]/gi,
    '<span style="color: $1;">$2</span>',
  )

  // 处理 [size] 标签，映射到具体的像素值
  text = text.replace(
    /\[size=(\d+)\]([\s\S]*?)\[\/size\]/gi,
    (match: string, size: string, content: string): string => {
      // 根据 size 值映射到具体的像素值
      const sizeMap: { [key: string]: string } = {
        '1': '12',
        '2': '14',
        '3': '16',
        '4': '18',
        '5': '24',
        '6': '32',
        '7': '48',
      }
      const pixelSize = sizeMap[size] || parseInt(size) * 4
      return `<span style="font-size: ${pixelSize}px;">${content}</span>`
    },
  )

  // 处理换行符
  text = text.replace(/\n/g, '<br>')

  return text
}

const handleApiError = (error: unknown, defaultMessage: string) => {
  const message = axios.isAxiosError(error)
    ? (error.response?.data as { logs?: string; error?: string; message?: string } | undefined)
        ?.logs ||
      (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
      (error.response?.data as { error?: string; message?: string } | undefined)?.message ||
      error.message ||
      defaultMessage
    : error instanceof Error
      ? error.message || defaultMessage
      : defaultMessage
  ElNotification.error({ title: '操作失败', message, duration: 0, showClose: true })
}

interface SiteStatus {
  name: string
  site: string
  has_cookie: boolean
  has_passkey: boolean
  is_source: boolean
  is_target: boolean
  uses_public_publisher?: boolean
  uses_public_extractor?: boolean
}

interface ScreenshotPreviewCandidateItem {
  id: string
  timeSeconds: number
  timeLabel: string
  previewData: string
}

const props = defineProps({
  showCompleteButton: {
    type: Boolean,
    default: false,
  },
  publishScene: {
    type: String,
    default: '',
  },
  prefetchedDbSeedInfo: {
    type: Object,
    default: null,
  },
})

const emit = defineEmits(['complete', 'cancel', 'close-with-refresh'])

const crossSeedStore = useCrossSeedStore()

const torrent = computed(() => crossSeedStore.workingParams as WorkingTorrent)
const sourceSite = computed(() => crossSeedStore.sourceInfo?.name || '')

const normalizeScreenshotReviewStatus = (value: unknown): ScreenshotReviewStatus => {
  if (typeof value !== 'string') return 'none'
  const normalized = value.trim().toLowerCase()
  if (normalized === 'pending' || normalized === 'confirmed') {
    return normalized
  }
  return 'none'
}

const getInitialTorrentData = (): TorrentData => ({
  seed_id: null,
  title_components: [] as { key: string; value: string }[],
  original_main_title: '',
  subtitle: '',
  imdb_link: '',
  douban_link: '',
  tmdb_link: '',
  screenshot_review_status: 'none',
  intro: { statement: '', poster: '', body: '', screenshots: '', removed_ardtudeclarations: [] },
  mediainfo: '',
  source_params: {},
  standardized_params: {
    type: '',
    medium: '',
    video_codec: '',
    audio_codec: '',
    resolution: '',
    team: '',
    source: '',
    tags: [] as string[],
  },
  final_publish_parameters: {},
  complete_publish_params: {},
  raw_params_for_preview: {},
})

const parseImageUrls = (value: string | string[] | null | undefined) => {
  if (!value) return []

  if (Array.isArray(value)) {
    return value
      .map((item) => String(item || '').trim())
      .filter((item) => item.startsWith('http://') || item.startsWith('https://'))
  }

  const text = String(value).trim()
  if (!text) return []

  const bbcodeRegex = /\[img\](https?:\/\/[^\s[\]]+)\[\/img\]/gi
  const bbcodeMatches = [...text.matchAll(bbcodeRegex)].map((match) => match[1])
  if (bbcodeMatches.length > 0) {
    return bbcodeMatches
  }

  return text
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter((item) => item.startsWith('http://') || item.startsWith('https://'))
}

// 图片代理URL处理
const imageProxyMap = ref(new Map<string, string>())

const getProxyImageUrl = (originalUrl: string): string => {
  // 如果已经尝试过代理URL，直接返回
  if (imageProxyMap.value.has(originalUrl)) {
    return imageProxyMap.value.get(originalUrl)!
  }

  // 首次尝试原始URL
  imageProxyMap.value.set(originalUrl, originalUrl)
  return originalUrl
}

const handleImageErrorWithProxy = (url: string, type: 'poster' | 'screenshot', index: number) => {
  // 检查是否已经尝试过代理
  const currentUrl = imageProxyMap.value.get(url)
  if (currentUrl && !currentUrl.startsWith('http://pt-nexus-proxy.sqing33.dpdns.org/')) {
    // 尝试使用代理URL
    const proxyUrl = `http://pt-nexus-proxy.sqing33.dpdns.org/${url}`
    imageProxyMap.value.set(url, proxyUrl)

    // 强制更新图片显示
    const imgElements = document.querySelectorAll(`img[src="${currentUrl}"]`)
    imgElements.forEach((img) => {
      img.setAttribute('src', proxyUrl)
    })

    console.log(`图片加载失败，尝试使用代理URL: ${proxyUrl}`)
    return // 不调用原有的错误处理，给代理URL一次机会
  }

  // 如果代理URL也失败了，调用原有的错误处理
  handleImageError(url, type, index)
}

const activeStep = ref(0)
const activeTab = ref('main')
const isScrolledToBottom = ref(false)
const previewBrowseThreshold = 0.7

// Progress tracking variables
const publishProgress = ref<ProgressCounter>({ current: 0, total: 0 })
const downloaderProgress = ref<ProgressCounter>({ current: 0, total: 0 })

// 🚫 发种限制提示
const limitAlert = ref<LimitAlert>({
  visible: false,
  title: '',
  message: '',
})

// 防抖函数
const debounce = <T extends (...args: unknown[]) => void>(func: T, wait: number) => {
  let timeout: ReturnType<typeof setTimeout> | null = null
  return (...args: Parameters<T>) => {
    if (timeout) {
      clearTimeout(timeout)
    }
    timeout = setTimeout(() => {
      func(...args)
    }, wait)
  }
}

// 检查是否达到可继续下一步的浏览阈值
const checkIfScrolledToBottom = debounce(() => {
  const panelContent = document.querySelector('.panel-content')
  if (panelContent) {
    const { scrollTop, scrollHeight, clientHeight } = panelContent
    const scrollableDistance = Math.max(scrollHeight - clientHeight, 0)

    if (scrollableDistance <= 5) {
      isScrolledToBottom.value = true
      return
    }

    const browseProgress = scrollTop / scrollableDistance
    isScrolledToBottom.value = browseProgress >= previewBrowseThreshold
  }
}, 100) // 100ms防抖

// 添加滚动事件监听器
const addScrollListener = () => {
  const panelContent = document.querySelector('.panel-content')
  if (panelContent) {
    panelContent.addEventListener('scroll', checkIfScrolledToBottom)
  }
}

// 移除滚动事件监听器
const removeScrollListener = () => {
  const panelContent = document.querySelector('.panel-content')
  if (panelContent) {
    panelContent.removeEventListener('scroll', checkIfScrolledToBottom)
  }
}

let cleanupDetailsTabsDragHandlers: (() => void) | null = null

const cleanupDetailsTabsDrag = () => {
  if (cleanupDetailsTabsDragHandlers) {
    cleanupDetailsTabsDragHandlers()
    cleanupDetailsTabsDragHandlers = null
  }
}

const setupDetailsTabsDrag = () => {
  cleanupDetailsTabsDrag()

  if (activeStep.value !== 0 || window.innerWidth > 768) {
    return
  }

  const tabsRoot = document.querySelector('.details-tabs') as HTMLElement | null
  if (!tabsRoot) return

  const navWrap = tabsRoot.querySelector('.el-tabs__nav-wrap') as HTMLElement | null
  const navScroll = tabsRoot.querySelector('.el-tabs__nav-scroll') as HTMLElement | null
  const nav = tabsRoot.querySelector('.el-tabs__nav') as HTMLElement | null
  const navPrev = tabsRoot.querySelector('.el-tabs__nav-prev') as HTMLElement | null
  const navNext = tabsRoot.querySelector('.el-tabs__nav-next') as HTMLElement | null

  if (!navWrap || !nav) return

  navPrev?.style.setProperty('display', 'none')
  navNext?.style.setProperty('display', 'none')
  navWrap.style.setProperty('overflow-x', 'auto')
  navWrap.style.setProperty('overflow-y', 'hidden')
  navWrap.style.setProperty('-webkit-overflow-scrolling', 'touch')
  navWrap.style.setProperty('touch-action', 'pan-x')
  navWrap.style.setProperty('padding', '0 4px')

  if (navScroll) {
    navScroll.style.setProperty('overflow', 'visible')
  }

  nav.style.setProperty('transform', 'none')
  nav.style.setProperty('width', 'max-content')
  nav.style.setProperty('display', 'inline-flex')
  nav.style.setProperty('flex-wrap', 'nowrap')

  let isDragging = false
  let hasDragged = false
  let pointerId: number | null = null
  let startX = 0
  let startScrollLeft = 0

  const onPointerDown = (event: PointerEvent) => {
    if (event.pointerType === 'mouse' && event.button !== 0) return
    isDragging = true
    hasDragged = false
    pointerId = event.pointerId
    startX = event.clientX
    startScrollLeft = navWrap.scrollLeft

    navWrap.classList.add('is-dragging-tabs')
    if (typeof navWrap.setPointerCapture === 'function') {
      try {
        navWrap.setPointerCapture(event.pointerId)
      } catch {
        // ignore
      }
    }
  }

  const onPointerMove = (event: PointerEvent) => {
    if (!isDragging) return
    if (pointerId !== null && event.pointerId !== pointerId) return

    const deltaX = event.clientX - startX
    if (Math.abs(deltaX) > 4) {
      hasDragged = true
    }

    navWrap.scrollLeft = startScrollLeft - deltaX
    if (hasDragged) {
      event.preventDefault()
    }
  }

  const onPointerEnd = (event: PointerEvent) => {
    if (!isDragging) return
    if (pointerId !== null && event.pointerId !== pointerId) return

    isDragging = false
    pointerId = null
    navWrap.classList.remove('is-dragging-tabs')
  }

  const onClickCapture = (event: Event) => {
    if (!hasDragged) return
    event.preventDefault()
    event.stopPropagation()
    hasDragged = false
  }

  navWrap.addEventListener('pointerdown', onPointerDown)
  navWrap.addEventListener('pointermove', onPointerMove, { passive: false })
  window.addEventListener('pointerup', onPointerEnd)
  window.addEventListener('pointercancel', onPointerEnd)
  navWrap.addEventListener('click', onClickCapture, true)

  cleanupDetailsTabsDragHandlers = () => {
    navWrap.removeEventListener('pointerdown', onPointerDown)
    navWrap.removeEventListener('pointermove', onPointerMove)
    window.removeEventListener('pointerup', onPointerEnd)
    window.removeEventListener('pointercancel', onPointerEnd)
    navWrap.removeEventListener('click', onClickCapture, true)
    navWrap.classList.remove('is-dragging-tabs')
  }
}

const rebindDetailsTabsDrag = debounce(() => {
  if (activeStep.value !== 0) {
    cleanupDetailsTabsDrag()
    return
  }

  nextTick(() => {
    setupDetailsTabsDrag()
  })
}, 120)

// 在组件挂载时添加监听器
onMounted(() => {
  void fetchCrossSeedSettings()
  void (async () => {
    // 先获取站点状态，避免 fetchTorrentInfo 内部再次触发 /api/sites/status
    await fetchSitesStatus()
    await fetchTorrentInfo(props.prefetchedDbSeedInfo || undefined)
  })()

  // 在下一个tick添加滚动监听器，确保DOM已经渲染
  nextTick(() => {
    if (activeStep.value === 0) {
      setupDetailsTabsDrag()
    }

    if (activeStep.value === 1) {
      addScrollListener()
      checkIfScrolledToBottom() // 初始检查
    }
  })

  window.addEventListener('resize', rebindDetailsTabsDrag)
})

// 监听活动步骤的变化
watch(activeStep, (newStep, oldStep) => {
  if (oldStep === 0) {
    cleanupDetailsTabsDrag()
  }
  if (oldStep === 1) {
    removeScrollListener()
  }

  if (newStep === 0) {
    nextTick(() => {
      setupDetailsTabsDrag()
    })
  }

  if (newStep === 1) {
    nextTick(() => {
      addScrollListener()
      checkIfScrolledToBottom() // 初始检查
    })
  }
})

const steps = [
  { title: '核对种子详情' },
  { title: '发布参数预览' },
  { title: '选择发布站点' },
  { title: '完成发布' },
]
const allSitesStatus = ref<SiteStatus[]>([])
const selectedTargetSites = ref<string[]>([])
const autoAddExistingToDownloader = ref(false)
const autoUpdateExistingTorrent = ref(false)
const isLoading = ref(false)
const isEnqueueing = ref(false)
const torrentData = ref<TorrentData>(getInitialTorrentData())
const taskId = ref<string | null>(null)
const finalResultsList = ref<RawPublishResult[]>([])
const publishResultsBySite = ref<Record<string, RawPublishResult | undefined>>({})
const publishingSites = ref<string[]>([])
const publishBatchId = ref<string | null>(null)
const publishBatchEventSource = ref<EventSource | null>(null)
const isReparsing = ref(false)
const isRefreshingScreenshots = ref(false)
const isConfirmingScreenshotReview = ref(false)
const isFinalizingScreenshotPreview = ref(false)
const showScreenshotPreviewDialog = ref(false)
const screenshotPreviewCandidates = ref<ScreenshotPreviewCandidateItem[]>([])
const activeScreenshotPreviewId = ref('')
const selectedScreenshotPreviewIds = ref<string[]>([])
const screenshotPreviewSelectionLimit = ref(5)
const isRefreshingIntro = ref(false)
const isRefreshingMediainfo = ref(false)
const isRefreshingPosters = ref(false)
const isHandlingScreenshotError = ref(false) // 防止重复处理截图错误
const screenshotValid = ref(true) // 跟踪截图是否有效
const logContent = ref('')
const showLogCard = ref(false)
const downloaderList = ref<{ id: string; name: string }[]>([])
const isDataFromDatabase = ref(false) // Flag to track if data was loaded from database

// BDInfo SSE相关变量
const bdinfoEventSource = ref<EventSourceLike | null>(null)

// BDInfo 进度相关变量
const bdinfoProgress = ref<BdinfoProgress>({
  visible: false,
  percent: 0,
  currentFile: '',
  elapsedTime: '',
  remainingTime: '',
})

// BDInfo 状态变量
const bdinfoStatus = ref('')

// BDInfo 碟片大小
const discSize = ref(0)

// 格式化文件大小
const formatFileSize = (bytes: number) => {
  if (!bytes) return ''

  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }

  return `${size.toFixed(2)} ${units[unitIndex]}`
}

// 日志进度组件相关
const showLogProgress = ref(false)
const logProgressTaskId = ref('')
const fetchFlowErrorMessage = ref('')

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
})

const posterImages = computed(() => parseImageUrls(torrentData.value.intro.poster))
const screenshotImages = computed(() => parseImageUrls(torrentData.value.intro.screenshots))
const isScreenshotReviewPending = computed(
  () => torrentData.value.screenshot_review_status === 'pending',
)
const hasRestrictedAutoRepairTags = computed(() => {
  const tags = torrentData.value.standardized_params.tags || []
  return tags.some(
    (tag) =>
      tag === '禁转' ||
      tag === 'tag.禁转' ||
      tag === '限转' ||
      tag === 'tag.限转' ||
      tag === '分集' ||
      tag === 'tag.分集',
  )
})

const buildScreenshotPayload = (type: string, extra: Record<string, unknown> = {}) => ({
  type,
  content_name: torrentData.value.original_main_title,
  source_info: {
    main_title: torrentData.value.original_main_title,
    source_site: sourceSite.value,
    imdb_link: torrentData.value.imdb_link,
    douban_link: torrentData.value.douban_link,
    tmdb_link: torrentData.value.tmdb_link,
  },
  savePath: torrent.value.save_path,
  torrentName: torrent.value.name,
  downloaderId: torrent.value.downloaderId,
  ...extra,
})

const normalizeScreenshotPreviewCandidates = (value: unknown): ScreenshotPreviewCandidateItem[] => {
  if (!Array.isArray(value)) return []
  return value
    .map((item, index) => {
      const candidate = item as Record<string, unknown>
      const id =
        typeof candidate.id === 'string' && candidate.id.trim()
          ? candidate.id.trim()
          : `candidate-${index}`
      const timeSeconds =
        typeof candidate.time_seconds === 'number'
          ? candidate.time_seconds
          : typeof candidate.time_seconds === 'string'
            ? Number(candidate.time_seconds)
            : 0
      const timeLabel =
        typeof candidate.time_label === 'string' && candidate.time_label.trim()
          ? candidate.time_label.trim()
          : '--:--:--'
      const previewData = typeof candidate.preview_data === 'string' ? candidate.preview_data : ''
      return { id, timeSeconds, timeLabel, previewData }
    })
    .filter((candidate) => candidate.previewData)
}

const applyScreenshotPreviewCandidates = (
  rawCandidates: unknown,
  rawSelectionLimit: unknown,
  notification?: { title: string; message: string; type?: 'success' | 'info' },
) => {
  const candidates = normalizeScreenshotPreviewCandidates(rawCandidates)
  const selectionLimit =
    typeof rawSelectionLimit === 'number'
      ? rawSelectionLimit
      : Number(rawSelectionLimit || 5)
  if (candidates.length === 0) {
    return false
  }

  screenshotPreviewCandidates.value = candidates
  activeScreenshotPreviewId.value = candidates[0]?.id || ''
  screenshotPreviewSelectionLimit.value = selectionLimit > 0 ? selectionLimit : 5
  torrentData.value.screenshot_review_status = 'pending'
  showScreenshotPreviewDialog.value = true

  if (notification) {
    ElNotification[notification.type || 'info']({
      title: notification.title,
      message: notification.message,
    })
  }
  return true
}

const openFetchedScreenshotPreview = async () => {
  if (!torrentData.value.original_main_title) return
  if (isRefreshingScreenshots.value || isFinalizingScreenshotPreview.value) return

  isRefreshingScreenshots.value = true
  ElNotification.info({
    title: '正在生成候选截图',
    message: '抓取过程中未检测到字幕流，正在生成候选截图供选择...',
    duration: 0,
  })

  try {
    const response = await axios.post(
      '/api/media/validate',
      buildScreenshotPayload('screenshot_preview', {
        preview_count: 12,
      }),
    )
    ElNotification.closeAll()
    if (
      response.data.success &&
      applyScreenshotPreviewCandidates(response.data.candidates, response.data.selection_limit, {
        title: '请选择截图候选',
        message: '当前未检测到字幕流，请直接在候选列表中选择 5 张截图。',
        type: 'success',
      })
    ) {
      return
    }

    ElNotification.error({
      title: '候选生成失败',
      message: response.data.error || '未能生成候选截图，请查看后台日志。',
    })
  } catch (error: unknown) {
    ElNotification.closeAll()
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
        (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        '未能生成候选截图，请查看后台日志。'
      : error instanceof Error
        ? error.message || '未能生成候选截图，请查看后台日志。'
        : '未能生成候选截图，请查看后台日志。'
    ElNotification.error({
      title: '候选生成失败',
      message: errorMsg,
    })
  } finally {
    isRefreshingScreenshots.value = false
  }
}

const resetScreenshotPreviewState = () => {
  screenshotPreviewCandidates.value = []
  activeScreenshotPreviewId.value = ''
  selectedScreenshotPreviewIds.value = []
  screenshotPreviewSelectionLimit.value = 5
}

const activeScreenshotPreviewCandidate = computed(
  () =>
    screenshotPreviewCandidates.value.find(
      (candidate) => candidate.id === activeScreenshotPreviewId.value,
    ) ||
    screenshotPreviewCandidates.value[0] ||
    null,
)

const setActiveScreenshotPreview = (id: string) => {
  activeScreenshotPreviewId.value = id
}

const isScreenshotPreviewSelected = (id: string) => selectedScreenshotPreviewIds.value.includes(id)

const toggleScreenshotPreviewSelection = (id: string) => {
  if (isFinalizingScreenshotPreview.value) return
  if (!activeScreenshotPreviewId.value) {
    activeScreenshotPreviewId.value = id
  }
  if (isScreenshotPreviewSelected(id)) {
    selectedScreenshotPreviewIds.value = selectedScreenshotPreviewIds.value.filter(
      (item) => item !== id,
    )
    return
  }
  if (selectedScreenshotPreviewIds.value.length >= screenshotPreviewSelectionLimit.value) {
    ElNotification.warning(`最多只能选择 ${screenshotPreviewSelectionLimit.value} 张候选截图。`)
    return
  }
  selectedScreenshotPreviewIds.value = [...selectedScreenshotPreviewIds.value, id]
}

const confirmScreenshotPreviewSelection = async () => {
  if (selectedScreenshotPreviewIds.value.length !== screenshotPreviewSelectionLimit.value) {
    ElNotification.warning(`请先选择 ${screenshotPreviewSelectionLimit.value} 张候选截图。`)
    return
  }

  const selectedTimes = screenshotPreviewCandidates.value
    .filter((candidate) => selectedScreenshotPreviewIds.value.includes(candidate.id))
    .sort((left, right) => left.timeSeconds - right.timeSeconds)
    .map((candidate) => candidate.timeSeconds)
  if (selectedTimes.length !== screenshotPreviewSelectionLimit.value) {
    ElNotification.error('候选截图时间点读取失败，请重新生成候选截图。')
    return
  }

  isFinalizingScreenshotPreview.value = true
  isRefreshingScreenshots.value = true
  ElNotification.info({
    title: '正在生成正式截图',
    message: '已按所选时间点开始生成正式截图并上传图床...',
    duration: 0,
  })

  try {
    const response = await axios.post(
      '/api/media/validate',
      buildScreenshotPayload('screenshot_finalize', {
        selected_times: selectedTimes,
      }),
    )
    ElNotification.closeAll()

    if (response.data.success && response.data.screenshots) {
      torrentData.value.intro.screenshots = response.data.screenshots
      torrentData.value.screenshot_review_status = 'confirmed'
      screenshotValid.value = true
      showScreenshotPreviewDialog.value = false
      ElNotification.success({
        title: '截图已更新',
        message: '已成功生成并加载所选时间点的正式截图。',
      })
      return
    }

    ElNotification.error({
      title: '正式截图生成失败',
      message: response.data.error || '后端未返回可用截图，请查看后台日志。',
    })
  } catch (error: unknown) {
    ElNotification.closeAll()
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
        (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        '未能生成正式截图，请查看后台日志。'
      : error instanceof Error
        ? error.message || '未能生成正式截图，请查看后台日志。'
        : '未能生成正式截图，请查看后台日志。'
    ElNotification.error({
      title: '正式截图生成失败',
      message: errorMsg,
    })
  } finally {
    isFinalizingScreenshotPreview.value = false
    isRefreshingScreenshots.value = false
  }
}

const filteredDeclarationsList = computed(() => {
  const removedDeclarations = torrentData.value.intro.removed_ardtudeclarations
  if (Array.isArray(removedDeclarations)) {
    return removedDeclarations
  }
  return []
})
const filteredDeclarationsCount = computed(() => filteredDeclarationsList.value.length)

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

const isCurrentSeedAnimationRelated = computed(() =>
  isAnimationRelatedType(torrentData.value.standardized_params.type),
)

const isIloliconSite = (siteStatus: SiteStatus | undefined) => {
  if (!siteStatus) return false
  return (
    String(siteStatus.site || '')
      .trim()
      .toLowerCase() === 'ilolicon' ||
    String(siteStatus.name || '')
      .trim()
      .toLowerCase() === 'ilolicon'
  )
}

const isPublicPublisherSite = (siteStatus: SiteStatus | undefined): boolean => {
  if (!siteStatus) return false
  if (typeof siteStatus.uses_public_publisher === 'boolean') {
    return siteStatus.uses_public_publisher
  }
  if (typeof siteStatus.uses_public_extractor === 'boolean') {
    return siteStatus.uses_public_extractor
  }
  return false
}

const isAutoUpdateHighlightSite = (siteStatus: SiteStatus | undefined): boolean => {
  if (!autoUpdateExistingTorrent.value) return false
  return isPublicPublisherSite(siteStatus)
}

const isTargetSiteSelectable = (siteName: string): boolean => {
  // 步骤 1: 查找站点的状态信息
  const siteStatus = allSitesStatus.value.find((s) => s.name === siteName)

  // 条件 1: 如果找不到站点信息，则不可选
  if (!siteStatus) {
    return false
  }

  // 肉丝站点不需要Cookie，其他站点需要配置Cookie
  if (siteName !== '肉丝' && !siteStatus.has_cookie) {
    return false
  }

  // 对于杜比(hddolby)和HDTime站点，还需要检查passkey
  if (
    (siteName === '杜比' || siteName === 'HDtime' || siteName === '肉丝') &&
    !siteStatus.has_passkey
  ) {
    return false
  }

  // 条件 2: 如果种子已经存在于该站点，且未开启“更新种子信息”，则不可选
  if (torrent.value?.sites?.[siteName] && !autoUpdateExistingTorrent.value) {
    return false
  }

  // 条件 3: ilolicon 仅支持动漫/动画相关内容
  if (isIloliconSite(siteStatus) && !isCurrentSeedAnimationRelated.value) {
    return false
  }

  // 条件 4: 检查是否为ubits站点并应用特殊禁转规则
  if (siteName.toLowerCase() === 'ubits') {
    const team = torrentData.value.standardized_params.team
    const titleComponents = torrentData.value.title_components

    // 检查标准化参数中的制作组
    if (
      team &&
      ['cmct', 'cmctv', 'hdsky', 'hdsweb', 'hds', 'hdstv', 'hdspad'].includes(team.toLowerCase())
    ) {
      return false
    }

    // 检查标题组件中的制作组
    const teamComponent = titleComponents.find((param) => param.key === '制作组')
    if (teamComponent && teamComponent.value) {
      const teamValue = teamComponent.value.toLowerCase()
      const forbiddenTeams = [
        'cmct',
        'cmctv',
        'telesto',
        'shadow610',
        'hdsky',
        'hdsweb',
        'hds',
        'hdstv',
        'hdspad',
      ]

      for (const forbiddenTeam of forbiddenTeams) {
        if (teamValue.includes(forbiddenTeam)) {
          return false
        }
      }
    }
  }

  // 如果所有检查都通过，则站点可选
  return true
}

const isUbitsDisabled = computed(() => {
  const team = torrentData.value.standardized_params.team
  const titleComponents = torrentData.value.title_components

  if (
    team &&
    ['cmct', 'cmctv', 'hdsky', 'hdsweb', 'hds', 'hdstv', 'hdspad'].includes(team.toLowerCase())
  ) {
    return true
  }

  const teamComponent = titleComponents.find((param) => param.key === '制作组')
  if (teamComponent && teamComponent.value) {
    const teamValue = teamComponent.value.toLowerCase()
    const forbiddenTeams = [
      'cmct',
      'cmctv',
      'telesto',
      'shadow610',
      'hdsky',
      'hdsweb',
      'hds',
      'hdstv',
      'hdspad',
    ]

    for (const forbiddenTeam of forbiddenTeams) {
      if (teamValue.includes(forbiddenTeam)) {
        return true
      }
    }
  }

  return false
})

// 新增函数：根据站点状态获取按钮类型
const getButtonType = (site: SiteStatus) => {
  // 如果站点已被选中，显示为绿色
  if (selectedTargetSites.value.includes(site.name)) {
    return 'success'
  }
  // 如果站点没有Cookie（肉丝站点除外），显示为红色 (danger)
  if (!site.has_cookie && site.name !== '肉丝') {
    return 'danger'
  }
  // 对于杜比、HDtime、肉丝站点，如果未配置Passkey，也显示为红色
  if (
    (site.name === '杜比' || site.name === 'HDtime' || site.name === '肉丝') &&
    !site.has_passkey
  ) {
    return 'danger'
  }
  // 其他情况（可选但未选中），显示为默认样式
  return 'default'
}

const refreshIntro = async () => {
  isRefreshingIntro.value = true
  ElNotification.info({
    title: '正在重新获取',
    message: '正在从豆瓣/IMDb/TMDb重新获取简介...',
    duration: 0,
  })

  const payload = {
    type: 'intro',
    content_name: torrentData.value.original_main_title,
    source_info: {
      main_title: torrentData.value.original_main_title,
      subtitle: torrentData.value.subtitle,
      source_site: sourceSite.value,
      imdb_link: torrentData.value.imdb_link,
      douban_link: torrentData.value.douban_link,
      tmdb_link: torrentData.value.tmdb_link,
    },
  }

  try {
    const response = await axios.post('/api/media/validate', payload)
    ElNotification.closeAll()

    if (response.data.success && response.data.intro) {
      const normalized = normalizeIntroBodyAndMediainfo(
        response.data.intro,
        torrentData.value.mediainfo,
      )
      torrentData.value.intro.body = normalized.body
      if (!torrentData.value.mediainfo || torrentData.value.mediainfo.trim() === '') {
        torrentData.value.mediainfo = normalized.mediainfo
      }

      // 合并后端从简介“类别”字段提取出的标准化标签（tag.*），并去重。
      const categoryTags = Array.isArray(response.data.category_tags)
        ? (response.data.category_tags as string[])
        : []
      if (categoryTags.length > 0) {
        const currentTags = torrentData.value.standardized_params.tags
        const merged = [...new Set([...currentTags, ...categoryTags])].filter(
          (tag) => tag && typeof tag === 'string' && tag.trim() !== '',
        )
        torrentData.value.standardized_params.tags = merged
      }

      // 若后端返回 type_override，则按返回值覆盖当前类型（用于动画类别联动）。
      if (response.data.type_override && typeof response.data.type_override === 'string') {
        const override = response.data.type_override.trim()
        if (override) {
          torrentData.value.standardized_params.type = override
        }
      }

      // 若后端从简介中提取到产地，则同步修正 standardized_params.source（不落库，随“下一步”统一提交）。
      if (response.data.source_override && typeof response.data.source_override === 'string') {
        const sourceOverride = response.data.source_override.trim()
        if (sourceOverride && sourceOverride !== 'source.other') {
          torrentData.value.standardized_params.source = sourceOverride
        }
      }

      // 使用返回的IMDb链接、豆瓣链接、TMDb链接填充
      if (response.data.extracted_imdb_link && !torrentData.value.imdb_link) {
        torrentData.value.imdb_link = response.data.extracted_imdb_link
      }

      if (response.data.extracted_douban_link && !torrentData.value.douban_link) {
        torrentData.value.douban_link = response.data.extracted_douban_link
      }

      if (response.data.extracted_tmdb_link && !torrentData.value.tmdb_link) {
        torrentData.value.tmdb_link = response.data.extracted_tmdb_link
      }

      ElNotification.success({
        title: '重新获取成功',
        message: '已成功从豆瓣/IMDb/TMDb获取并更新了简介内容。',
      })
    } else {
      ElNotification.error({
        title: '重新获取失败',
        message: response.data.error || '无法从豆瓣/IMDb/TMDb获取简介。',
      })
    }
  } catch (error: unknown) {
    ElNotification.closeAll()
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
        (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        '未能重新获取简介'
      : error instanceof Error
        ? error.message || '未能重新获取简介'
        : '未能重新获取简介'
    ElNotification.error({
      title: '操作失败',
      message: errorMsg,
    })
  } finally {
    isRefreshingIntro.value = false
  }
}

type InferredStandardizedParams = Partial<
  Pick<StandardizedParams, 'video_codec' | 'audio_codec' | 'resolution'>
>

type SeedUpdates = {
  title_components?: TitleComponent[]
  standardized_params?: Partial<StandardizedParams>
  inferred_standardized_params?: InferredStandardizedParams
}

const standardParamLabels: Record<keyof InferredStandardizedParams, string> = {
  video_codec: '视频编码',
  audio_codec: '音频编码',
  resolution: '分辨率',
}

const applyInferredStandardizedParams = (
  inferred: unknown,
): Array<keyof InferredStandardizedParams> => {
  if (!inferred || typeof inferred !== 'object') return []
  const params = inferred as InferredStandardizedParams
  const changed: Array<keyof InferredStandardizedParams> = []

  ;(['video_codec', 'audio_codec', 'resolution'] as const).forEach((key) => {
    const candidate = typeof params[key] === 'string' ? params[key].trim() : ''
    if (!candidate || candidate.endsWith('.other')) return

    const current = torrentData.value.standardized_params[key].trim()
    if (current === candidate) return

    torrentData.value.standardized_params[key] = candidate
    changed.push(key)
  })

  return changed
}

const formatCorrectedStandardParams = (
  keys: Array<keyof InferredStandardizedParams>,
): string => {
  const uniqueKeys = [...new Set(keys)]
  if (uniqueKeys.length === 0) return ''
  return uniqueKeys.map((key) => standardParamLabels[key]).join('、')
}

const applySeedUpdates = (seedUpdates: unknown): Array<keyof InferredStandardizedParams> => {
  if (!seedUpdates || typeof seedUpdates !== 'object') return []
  const updates = seedUpdates as SeedUpdates

  // 1) 标题组件/媒介等：直接同步后端回传的最新值
  if (Array.isArray(updates.title_components)) {
    torrentData.value.title_components = updates.title_components
  }

  const standardized = updates.standardized_params
  if (standardized && typeof standardized === 'object') {
    if (typeof standardized.medium === 'string' && standardized.medium.trim() !== '') {
      torrentData.value.standardized_params.medium = standardized.medium.trim()
    }

    // tags 合并 + 去重（尽量不覆盖用户本地手工调整）
    const returnedTags = Array.isArray(standardized.tags) ? standardized.tags : []
    if (returnedTags.length > 0) {
      const currentTags = torrentData.value.standardized_params.tags
      const merged = [...new Set([...currentTags, ...returnedTags])]
        .map((t) => (typeof t === 'string' ? t.trim() : ''))
        .filter((t) => t !== '')
      torrentData.value.standardized_params.tags = merged
    }

    // type 默认不主动覆盖（除非当前为空），避免打断用户已选择的类型
    if (typeof standardized.type === 'string' && standardized.type.trim() !== '') {
      const currentType = torrentData.value.standardized_params.type.trim()
      if (!currentType) {
        torrentData.value.standardized_params.type = standardized.type.trim()
      }
    }
  }

  // 2) 编码/分辨率推断：有有效值就直接覆盖当前种子信息参数。
  return applyInferredStandardizedParams(updates.inferred_standardized_params)
}

const refreshScreenshots = async () => {
  if (!torrentData.value.original_main_title) {
    ElNotification.warning('标题为空，无法重新获取截图。')
    return
  }

  if (isRefreshingScreenshots.value || isFinalizingScreenshotPreview.value) {
    ElNotification.info({
      title: '正在处理中',
      message: '截图预览或正式生成已在处理中，请稍候...',
    })
    return
  }

  isRefreshingScreenshots.value = true
  ElNotification.info({
    title: '正在重新获取截图',
    message: '正在检查字幕流；若未找到字幕流，将生成候选截图供手动选择...',
    duration: 0,
  })

  try {
    const response = await axios.post(
      '/api/media/validate',
      buildScreenshotPayload('screenshot', {
        preview_count: 12,
      }),
    )
    ElNotification.closeAll()

    if (response.data.success && response.data.screenshots) {
      torrentData.value.intro.screenshots = response.data.screenshots
      torrentData.value.screenshot_review_status = 'none'
      screenshotValid.value = true
      ElNotification.success({
        title: '重新获取成功',
        message: '已根据字幕流自动生成并加载新的截图。',
      })
    } else if (
      response.data.success &&
      applyScreenshotPreviewCandidates(response.data.candidates, response.data.selection_limit, {
        title: '候选已生成',
        message: '未检测到字幕流，请手动选择 5 张截图后再生成正式截图。',
        type: 'success',
      })
    ) {
      return
    } else {
      ElNotification.error({
        title: '候选生成失败',
        message: response.data.error || '无法从后端获取候选截图，请查看后台日志。',
      })
    }
  } catch (error: unknown) {
    ElNotification.closeAll()
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
        (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        '未能生成候选截图，请查看后台日志。'
      : error instanceof Error
        ? error.message || '未能生成候选截图，请查看后台日志。'
        : '未能生成候选截图，请查看后台日志。'
    ElNotification.error({
      title: '候选生成失败',
      message: errorMsg,
    })
  } finally {
    isRefreshingScreenshots.value = false
  }
}

const refreshMediainfo = async () => {
  // 移除标题检查，允许任何时候重新获取
  // 防止重复请求
  if (isRefreshingMediainfo.value) {
    ElNotification.info({
      title: '正在处理中',
      message: '媒体信息重新获取请求已在处理中，请稍候...',
    })
    return
  }

  isRefreshingMediainfo.value = true
  ElNotification.info({
    title: '正在重新获取',
    message: '正在从视频重新生成媒体信息...',
    duration: 0,
  })

  try {
    // 使用新的异步 API
    const response = await axios.post('/api/migrate/refresh_mediainfo_async', {
      seed_id: torrentData.value.seed_id,
      save_path: torrent.value.save_path,
      content_name: torrentData.value.original_main_title,
      downloader_id: torrent.value.downloaderId,
      torrent_name: torrent.value.name,
      current_mediainfo: torrentData.value.mediainfo,
      force_refresh: true,
      priority: 1, // 单个种子使用高优先级
    })

    ElNotification.closeAll()

    if (response.data.success) {
      // 如果有 MediaInfo 内容，先更新
      if (response.data.mediainfo) {
        torrentData.value.mediainfo = response.data.mediainfo
      }

      // 同步后端回传的最新字段更新（标签/标题组件/媒介/推断编码分辨率等）。
      const correctedKeys = response.data.seed_updates
        ? applySeedUpdates(response.data.seed_updates)
        : []
      const correctedText = formatCorrectedStandardParams(correctedKeys)

      // 如果 BDInfo 在后台处理中，开始SSE连接
      if (response.data.bdinfo_async && response.data.bdinfo_async.bdinfo_status === 'processing') {
        ElNotification.info({
          title: 'BDInfo 处理中',
          message: 'BDInfo 正在后台处理中，完成后将自动更新...',
          duration: 5000,
        })
        startBDInfoSSE()
      } else if (response.data.mediainfo) {
        ElNotification.success({
          title: '重新获取成功',
          message:
            correctedText !== ''
              ? `已成功生成并加载新的媒体信息，并修正${correctedText}。`
              : response.data.message || '已成功生成并加载了新的媒体信息。',
        })
      } else {
        ElNotification.info({
          title: '任务已启动',
          message: response.data.message || 'BDInfo 正在后台处理中...',
        })
      }
    } else {
      ElNotification.error({
        title: '重新获取失败',
        message: response.data.message || '无法从后端获取新的媒体信息，请查看后台日志。',
      })
    }
  } catch (error: unknown) {
    ElNotification.closeAll()
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string; error?: string } | undefined)?.message ||
        (error.response?.data as { error?: string } | undefined)?.error ||
        error.message ||
        '未能重新获取媒体信息，请查看后台日志。'
      : error instanceof Error
        ? error.message || '未能重新获取媒体信息，请查看后台日志。'
        : '未能重新获取媒体信息，请查看后台日志。'
    ElNotification.error({
      title: '操作失败',
      message: errorMsg,
    })
  } finally {
    isRefreshingMediainfo.value = false
  }
}

// 检查 BDInfo 状态并自动启动进度显示
const checkAndStartBDInfoProgress = async (seedId: string, isFromFetch: boolean = false) => {
  const maxRetries = isFromFetch ? 5 : 3 // 从抓取流程调用时增加重试次数
  const retryDelay = isFromFetch ? 2000 : 1000 // 从抓取流程调用时增加延迟

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      const response = await axios.get(`/api/migrate/bdinfo_status/${seedId}`)

      // 添加调试信息
      console.log(`BDInfo 状态 API 响应 (尝试 ${attempt}/${maxRetries}):`, response.data)

      // 修复：直接检查响应数据，不依赖 success 字段
      const data = response.data
      if (data && !data.error) {
        // 修复：从正确的字段获取状态
        const status = data.mediainfo_status || data.task_status?.status

        if (status === 'processing_bdinfo') {
          const taskId = data.bdinfo_task_id
          if (taskId) {
            // 启动 BDInfo 进度显示
            console.log(`检测到 BDInfo 任务正在进行中: ${status}`)
            console.log('任务 ID:', taskId)
            console.log('进度信息:', data.progress_info)

            startBDInfoSSE()
            bdinfoStatus.value = status
            return // 成功检测到任务，退出重试循环
          }
          console.log(`状态为 processing_bdinfo 但缺少任务ID，继续等待: ${seedId}`)
        } else if (status === 'queued') {
          // queued 表示待处理，不等于 BDInfo 已启动
          console.log(`任务处于 queued，等待后端修复流程继续推进: ${seedId}`)
        } else if (status === 'completed' || status === 'failed') {
          console.log(`BDInfo 任务已结束: ${status}，无需启动进度显示`)
          return // 任务已结束，退出重试循环
        } else {
          console.log(`BDInfo 任务状态: ${status}，尝试 ${attempt}/${maxRetries}`)
        }
      } else {
        console.warn('BDInfo 状态 API 返回错误:', data?.error)
      }
    } catch (error: unknown) {
      if (axios.isAxiosError(error)) {
        const status = error.response?.status
        if (status === 404) {
          console.warn(`种子记录不存在: ${seedId} (尝试 ${attempt}/${maxRetries})`)
        } else if (status === 500) {
          console.warn('服务器内部错误，检查 BDInfo 状态失败')
        } else if (typeof status === 'number') {
          console.warn(`HTTP ${status}: 检查 BDInfo 状态失败`)
        } else if (error.request) {
          console.warn('网络连接问题，无法检查 BDInfo 状态')
        } else {
          console.warn('检查 BDInfo 状态失败:', error.message)
        }
      } else if (error instanceof Error) {
        console.warn('检查 BDInfo 状态失败:', error.message)
      } else {
        console.warn('检查 BDInfo 状态失败:', error)
      }
    }

    // 如果不是最后一次尝试，等待后重试
    if (attempt < maxRetries) {
      console.log(`等待 ${retryDelay}ms 后重试检查 BDInfo 状态...`)
      await new Promise((resolve) => setTimeout(resolve, retryDelay))
    }
  }

  // 所有重试都失败了
  console.warn(`经过 ${maxRetries} 次尝试，未能检测到 BDInfo 任务`)
}

// BDInfo SSE相关函数
const startBDInfoSSE = () => {
  console.log('启动 BDInfo SSE 连接...')

  // 验证 seed_id
  if (!torrentData.value?.seed_id) {
    console.error('seed_id 未设置，无法建立 SSE 连接')
    ElNotification.error({
      title: '连接错误',
      message: '种子ID未设置，无法建立进度连接',
    })
    return
  }

  console.log(`使用 seed_id 建立 SSE 连接: ${torrentData.value.seed_id}`)

  // 关闭之前的连接
  stopBDInfoSSE(false)

  // 重置原盘体积，避免沿用上一次任务的值
  discSize.value = 0

  // 显示进度条
  bdinfoProgress.value = {
    visible: true,
    percent: 0,
    currentFile: '正在连接...',
    elapsedTime: '',
    remainingTime: '',
  }

  // 创建EventSource连接
  const url = `/api/migrate/bdinfo_sse/${torrentData.value.seed_id}`
  console.log(`SSE 连接 URL: ${url}`)
  bdinfoEventSource.value = openSSE(url)

  // 添加连接超时处理
  let connectionTimeout: ReturnType<typeof setTimeout> | null = setTimeout(() => {
    if (bdinfoEventSource.value?.readyState === EventSource.CONNECTING) {
      console.warn('SSE 连接超时，尝试重新连接')
      bdinfoEventSource.value?.close()
      // 尝试重新连接一次
      if (bdinfoProgress.value.visible) {
        setTimeout(() => {
          console.log('尝试重新建立 SSE 连接...')
          startBDInfoSSE()
        }, 2000)
      }
    }
  }, 5000) // 5秒超时

  // 处理连接成功
  bdinfoEventSource.value.onopen = () => {
    console.log('BDInfo SSE连接已建立')
    if (connectionTimeout) {
      clearTimeout(connectionTimeout)
      connectionTimeout = null
    }
    // 请求当前进度状态
    requestCurrentProgress()
  }

  // 处理消息
  bdinfoEventSource.value.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)

      switch (data.type) {
        case 'connected':
          console.log('SSE连接成功:', data.connection_id)
          break

        case 'progress_update':
          // 更新进度条
          const { progress_percent, current_file, elapsed_time, remaining_time, disc_size } =
            data.data
          bdinfoProgress.value = {
            visible: true,
            percent: Math.round(progress_percent),
            currentFile: current_file,
            elapsedTime: elapsed_time,
            remainingTime: remaining_time,
          }
          // 更新disc size
          if (disc_size) {
            discSize.value = disc_size
          }
          console.log(`BDInfo 进度: ${progress_percent}%`)
          break

        case 'completion':
          // BDInfo 完成
          torrentData.value.mediainfo = data.data.mediainfo
          if (data.data.seed_updates) {
            applySeedUpdates(data.data.seed_updates)
          }
          ElNotification.success({
            title: 'BDInfo 获取完成',
            message: 'BDInfo 已成功获取并更新',
          })
          bdinfoProgress.value.visible = false
          stopBDInfoSSE(false)
          break

        case 'error':
          // BDInfo 失败
          ElNotification.warning({
            title: 'BDInfo 获取失败',
            message: data.data.error || 'BDInfo 获取失败，可手动重试',
          })
          bdinfoProgress.value.visible = false
          stopBDInfoSSE(false)
          break

        case 'heartbeat':
          // 心跳包，保持连接，不更新进度
          return

        default:
          console.log('未知SSE消息类型:', data.type)
      }
    } catch (error) {
      console.error('解析SSE消息失败:', error)
    }
  }

  // 处理错误
  bdinfoEventSource.value.onerror = (error) => {
    console.error('SSE连接错误:', error)
    if (connectionTimeout) {
      clearTimeout(connectionTimeout)
      connectionTimeout = null
    }

    // 检查连接状态
    const readyState = bdinfoEventSource.value?.readyState
    console.log(`SSE 连接状态: ${readyState} (0=CONNECTING, 1=OPEN, 2=CLOSED)`)

    // 如果是连接中或已关闭，尝试重连
    if (readyState === EventSource.CONNECTING || readyState === EventSource.CLOSED) {
      if (bdinfoProgress.value.visible) {
        console.log('尝试重新建立 SSE 连接...')
        bdinfoProgress.value.currentFile = '连接中断，正在重连...'

        // 延迟2秒后重连
        setTimeout(() => {
          if (bdinfoProgress.value.visible) {
            startBDInfoSSE()
          }
        }, 2000)
      }
    } else {
      // 其他错误，显示错误通知
      ElNotification.error({
        title: '连接错误',
        message: 'BDInfo 进度更新连接中断，请刷新页面重试',
      })
      bdinfoProgress.value.visible = false
      stopBDInfoSSE(false)
    }
  }
}

// 停止 BDInfo SSE
const stopBDInfoSSE = (showNotification: boolean | Event = true) => {
  if (bdinfoEventSource.value) {
    bdinfoEventSource.value.close()
    bdinfoEventSource.value = null
  }
  // 隐藏进度条
  bdinfoProgress.value.visible = false
  if (showNotification === true || (typeof showNotification === 'object' && showNotification)) {
    ElNotification.info({
      title: '已取消',
      message: 'BDInfo 获取已取消',
    })
  }
}

// 请求当前进度
const requestCurrentProgress = async () => {
  if (!torrentData.value?.seed_id) {
    console.warn('seed_id 未设置，无法请求当前进度')
    return
  }

  try {
    console.log('请求当前 BDInfo 进度状态...')
    const response = await axios.get(`/api/migrate/bdinfo_status/${torrentData.value.seed_id}`)

    if (response.data && response.data.task_status) {
      const taskStatus = response.data.task_status
      console.log('获取到当前进度状态:', taskStatus)

      // 回填原盘体积（byte）
      if (taskStatus.disc_size) {
        discSize.value = taskStatus.disc_size
      }

      // 如果任务正在进行中，更新进度显示
      if (taskStatus.status === 'processing_bdinfo') {
        bdinfoProgress.value = {
          visible: true,
          percent: Math.round(taskStatus.progress_percent || 0),
          currentFile: taskStatus.current_file || '处理中...',
          elapsedTime: taskStatus.elapsed_time || '',
          remainingTime: taskStatus.remaining_time || '',
        }
        console.log(`更新进度显示: ${taskStatus.progress_percent || 0}%`)
      }
    }
  } catch (error) {
    console.error('请求当前进度失败:', error)
    // 静默失败，不影响主要功能
  }
}

// 后台运行
const runInBackground = () => {
  // 停止SSE连接但保持任务运行
  if (bdinfoEventSource.value) {
    bdinfoEventSource.value.close()
    bdinfoEventSource.value = null
  }
  handleCancelClick()
}

// 在组件卸载时清理轮询
onUnmounted(() => {
  cleanupDetailsTabsDrag()
  window.removeEventListener('resize', rebindDetailsTabsDrag)
  stopPublishBatchSSE()
  if (bdinfoEventSource.value) {
    bdinfoEventSource.value.close()
    bdinfoEventSource.value = null
  }
})

const refreshPosters = async () => {
  if (!torrentData.value.original_main_title) {
    ElNotification.warning('标题为空，无法重新获取海报。')
    return
  }

  // 防止重复请求
  if (isRefreshingPosters.value) {
    ElNotification.info({
      title: '正在处理中',
      message: '海报重新获取请求已在处理中，请稍候...',
    })
    return
  }

  isRefreshingPosters.value = true
  ElNotification.info({
    title: '正在重新获取',
    message: '正在重新生成海报...',
    duration: 0,
  })

  const payload = {
    type: 'poster',
    content_name: torrentData.value.original_main_title,
    source_info: {
      main_title: torrentData.value.original_main_title,
      source_site: sourceSite.value,
      imdb_link: torrentData.value.imdb_link,
      douban_link: torrentData.value.douban_link,
      tmdb_link: torrentData.value.tmdb_link,
    },
    savePath: torrent.value.save_path,
    torrentName: torrent.value.name,
    downloaderId: torrent.value.downloaderId, // 添加下载器ID
  }

  try {
    const response = await axios.post('/api/media/validate', payload)
    ElNotification.closeAll()

    if (response.data.success && response.data.posters) {
      torrentData.value.intro.poster = response.data.posters

      // 同时更新链接（如果返回了的话）
      if (response.data.extracted_imdb_link && !torrentData.value.imdb_link) {
        torrentData.value.imdb_link = response.data.extracted_imdb_link
      }
      if (response.data.extracted_douban_link && !torrentData.value.douban_link) {
        torrentData.value.douban_link = response.data.extracted_douban_link
      }
      if (response.data.extracted_tmdb_link && !torrentData.value.tmdb_link) {
        torrentData.value.tmdb_link = response.data.extracted_tmdb_link
      }

      ElNotification.success({
        title: '重新获取成功',
        message: '已成功生成并加载了新的海报。',
      })
    } else {
      ElNotification.error({
        title: '重新获取失败',
        message: response.data.error || '无法从后端获取新的海报，请查看后台日志。',
      })
    }
  } catch (error: unknown) {
    ElNotification.closeAll()
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
        (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        '未能重新获取海报，请查看后台日志。'
      : error instanceof Error
        ? error.message || '未能重新获取海报，请查看后台日志。'
        : '未能重新获取海报，请查看后台日志。'
    ElNotification.error({
      title: '操作失败',
      message: errorMsg,
    })
  } finally {
    isRefreshingPosters.value = false
  }
}

const reparseTitle = async () => {
  if (!torrentData.value.original_main_title) {
    ElNotification.warning('标题为空，无法解析。')
    return
  }
  isReparsing.value = true
  try {
    const response = await axios.post('/api/utils/parse_title', {
      title: torrentData.value.original_main_title,
      mediainfo: torrentData.value.mediainfo || '', // 传递 mediainfo 以便修正 Blu-ray/BluRay 格式
    })
    if (response.data.success) {
      torrentData.value.title_components = response.data.components
      const correctedKeys = applyInferredStandardizedParams(
        response.data.inferred_standardized_params,
      )
      const correctedText = formatCorrectedStandardParams(correctedKeys)

      if (correctedText) {
        ElNotification.success(`标题已重新解析，并已同步修正${correctedText}。`)
      } else {
        ElNotification.success('标题已重新解析！')
      }
    } else {
      ElNotification.error(response.data.message || '解析失败')
    }
  } catch (error) {
    handleApiError(error, '未能重新解析标题，请查看后台日志。')
  } finally {
    isReparsing.value = false
  }
}

const handleImageError = async (url: string, type: 'poster' | 'screenshot', index: number) => {
  if (hasRestrictedAutoRepairTags.value) {
    console.log(`受限标签已命中，跳过失效${type === 'poster' ? '海报' : '截图'}自动修复: ${url}`)
    if (type === 'screenshot') {
      screenshotValid.value = true
    }
    return
  }

  // 如果是 pixhost.to 的图片，跳过检测
  if (url && url.includes('pixhost.to')) {
    console.log(`检测到 pixhost.to 图片，跳过有效性检测: ${url}`)
    return
  }

  // 防止重复处理截图错误
  if (type === 'screenshot' && isHandlingScreenshotError.value) {
    console.log(`截图错误已正在处理中，跳过重复请求: ${url}`)
    return
  }

  console.error(`图片加载失败: 类型=${type}, URL=${url}, 索引=${index}`)
  if (type === 'screenshot') {
    isHandlingScreenshotError.value = true
    screenshotValid.value = false // 标记截图无效
    ElNotification.warning({
      title: '截图失效',
      message: '检测到截图链接失效，正在尝试从视频重新生成...',
    })
  } else if (type === 'poster') {
    ElNotification.warning({
      title: '海报失效',
      message: '检测到海报链接失效，正在尝试重新获取...',
    })
  }

  const payload = {
    type: type,
    content_name: torrentData.value.original_main_title,
    source_info: {
      main_title: torrentData.value.original_main_title,
      source_site: sourceSite.value,
      imdb_link: torrentData.value.imdb_link,
      douban_link: torrentData.value.douban_link,
      tmdb_link: torrentData.value.tmdb_link,
    },
    savePath: torrent.value.save_path,
    torrentName: torrent.value.name,
    downloaderId: torrent.value.downloaderId, // 添加下载器ID
  }

  try {
    const response = await axios.post('/api/media/validate', payload)
    if (response.data.success) {
      if (type === 'screenshot' && response.data.screenshots) {
        torrentData.value.intro.screenshots = response.data.screenshots
        torrentData.value.screenshot_review_status = 'none'
        screenshotValid.value = true // 标记截图有效
        ElNotification.success({
          title: '截图已更新',
          message: '已成功生成并加载了新的截图。',
        })
      } else if (
        type === 'screenshot' &&
        applyScreenshotPreviewCandidates(response.data.candidates, response.data.selection_limit, {
          title: '需要手动选择截图',
          message: '当前未检测到字幕流，请在候选列表中选择 5 张截图。',
          type: 'info',
        })
      ) {
        return
      } else if (type === 'poster' && response.data.posters) {
        torrentData.value.intro.poster = response.data.posters
        ElNotification.success({
          title: '海报已更新',
          message: '已成功生成并加载了新的海报。',
        })
      }
    } else {
      // 如果更新截图失败，保持screenshotValid为false
      if (type === 'screenshot') {
        screenshotValid.value = false
      }
      ElNotification.error({
        title: '更新失败',
        message:
          response.data.error || `无法从后端获取新的${type === 'poster' ? '海报' : '截图'}。`,
      })
    }
  } catch (error: unknown) {
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { error?: string; message?: string } | undefined)?.error ||
        (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        `发送失效${type === 'poster' ? '海报' : '截图'}信息请求时发生错误，请查看后台日志。`
      : error instanceof Error
        ? error.message ||
          `发送失效${type === 'poster' ? '海报' : '截图'}信息请求时发生错误，请查看后台日志。`
        : `发送失效${type === 'poster' ? '海报' : '截图'}信息请求时发生错误，请查看后台日志。`
    console.error('发送失效图片信息请求时发生错误:', error)
    ElNotification.error({
      title: '操作失败',
      message: errorMsg,
    })
  } finally {
    // 重置截图处理状态
    if (type === 'screenshot') {
      isHandlingScreenshotError.value = false
      // 注意：不重置 screenshotValid 状态，保持当前的截图有效状态
    }
  }
}

// 通过中文站点名获取英文站点名，用于数据库查询

// --- Extracted: seed fetch/preview/site selection (see cross-seed/panel/seedFlow.ts) ---
const seedFlow = createSeedFlow({
  emit,
  sourceSite,
  torrent,
  crossSeedStore,

  allSitesStatus,
  selectedTargetSites,
  downloaderList,

  autoAddExistingToDownloader,
  autoUpdateExistingTorrent,

  activeStep,
  isLoading,
  isDataFromDatabase,
  taskId,

  torrentData,
  reverseMappings,

  logProgressTaskId,
  showLogProgress,

  fetchFlowErrorMessage,

  filterExtraEmptyLines,
  normalizeIntroBodyAndMediainfo,

  checkAndStartBDInfoProgress,
  openFetchedScreenshotPreview,
  handleApiError,
  isTargetSiteSelectable,

  screenshotValid,
  screenshotImages,
})

const {
  getEnglishSiteName,
  fetchSitesStatus,
  fetchCrossSeedSettings,
  saveAutoAddExistingSetting,
  saveAutoUpdateExistingTorrentSetting,
  fetchTorrentInfo,
  handleTeamInput,
  goToPublishPreviewStep,
  goToSelectSiteStep,
  toggleSiteSelection,
  selectAllTargetSites,
  clearAllTargetSites,
  invalidStandardParams,
  allTagOptions,
} = seedFlow

const resolveCurrentSeedLookupIdentity = async (): Promise<{
  torrentId: string
  siteName: string
} | null> => {
  const currentTorrent = torrent.value
  if (!currentTorrent) return null

  if (isDataFromDatabase.value && taskId.value && taskId.value.startsWith('db_')) {
    const parts = taskId.value.split('_')
    if (parts.length >= 3) {
      return {
        torrentId: parts[1],
        siteName: parts.slice(2).join('_'),
      }
    }
  }

  if (taskId.value && taskId.value.startsWith('db_')) {
    const parts = taskId.value.split('_')
    if (parts.length >= 3) {
      return {
        torrentId: parts[1],
        siteName: parts.slice(2).join('_'),
      }
    }
  }

  const siteDetails = currentTorrent.sites?.[sourceSite.value]
  if (!siteDetails) {
    return null
  }

  let torrentId = siteDetails.torrentId || ''
  if (!torrentId) {
    const idMatch = siteDetails.comment?.match(/id=(\d+)/)
    if (idMatch?.[1]) {
      torrentId = idMatch[1]
    }
  }
  const siteName = await getEnglishSiteName(sourceSite.value)
  if (!torrentId || !siteName) {
    return null
  }
  return { torrentId, siteName }
}

const confirmScreenshotReview = async () => {
  if (!torrentData.value.intro.screenshots.trim()) {
    ElNotification.warning('当前没有可确认的截图，请先重新获取截图。')
    return
  }

  const identity = await resolveCurrentSeedLookupIdentity()
  if (!identity) {
    ElNotification.error({
      title: '确认失败',
      message: '无法定位当前种子的 torrent_id 或 site_name，请刷新后重试。',
    })
    return
  }

  isConfirmingScreenshotReview.value = true
  try {
    const response = await axios.post('/api/migrate/update_screenshot_review_status', {
      torrent_id: identity.torrentId,
      site_name: identity.siteName,
      screenshot_review_status: 'confirmed',
    })
    if (response.data.success) {
      torrentData.value.screenshot_review_status = normalizeScreenshotReviewStatus(
        response.data.screenshot_review_status,
      )
      ElNotification.success({
        title: '截图已确认',
        message: '已确认当前截图时间点，可继续下一步。',
      })
      return
    }

    ElNotification.error({
      title: '确认失败',
      message: response.data.message || '截图确认状态更新失败，请查看后台日志。',
    })
  } catch (error: unknown) {
    const errorMsg = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string } | undefined)?.message ||
        error.message ||
        '截图确认状态更新失败，请查看后台日志。'
      : error instanceof Error
        ? error.message || '截图确认状态更新失败，请查看后台日志。'
        : '截图确认状态更新失败，请查看后台日志。'
    ElNotification.error({
      title: '确认失败',
      message: errorMsg,
    })
  } finally {
    isConfirmingScreenshotReview.value = false
  }
}

watch(isCurrentSeedAnimationRelated, (isAnimationRelated) => {
  if (isAnimationRelated) {
    return
  }

  selectedTargetSites.value = selectedTargetSites.value.filter((siteName) => {
    const siteStatus = allSitesStatus.value.find((s) => s.name === siteName)
    return !isIloliconSite(siteStatus)
  })
})

watch(autoUpdateExistingTorrent, (isEnabled) => {
  if (isEnabled) {
    return
  }

  selectedTargetSites.value = selectedTargetSites.value.filter((siteName) =>
    isTargetSiteSelectable(siteName),
  )
})

// --- Extracted: publish flow + derived computed (see cross-seed/panel/publishFlow.ts) ---
const publishFlow = createPublishFlow({
  emit,
  publishScene: props.publishScene,

  sourceSite,
  torrent,

  activeStep,
  isScrolledToBottom,

  isLoading,
  isEnqueueing,

  taskId,
  torrentData,
  reverseMappings,

  publishBatchId,
  publishBatchEventSource,

  publishProgress,
  downloaderProgress,
  limitAlert,

  selectedTargetSites,

  autoAddExistingToDownloader,
  autoUpdateExistingTorrent,

  downloaderList,

  finalResultsList,
  publishResultsBySite,
  publishingSites,

  logContent,
  showLogCard,

  logProgressTaskId,
  showLogProgress,

  goToSelectSiteStep,

  invalidStandardParams,
  screenshotValid,
})

const {
  stopPublishBatchSSE,
  handleLogProgressComplete,
  handleLogProgressClose,
  handlePublish,
  handleEnqueue,
  handlePreviousStep,
  handleCancelClick,
  handleScrollOrNextStep,
  handleCompleteClick,
  getMappedValue,
  getMappedTags,
  filteredTitleComponents,
  initialTitleComponents,
  unrecognizedValue,
  filteredTags,
  invalidTagsList,
  isRestrictedTag,
  getTagType,
  handleTagClose,
  isNextButtonDisabled,
  nextButtonTooltipContent,
  groupedResults,
  showSiteLog,
  filterUploadedParam,
  hasValidUrlsInRow,
  openAllSitesInRow,
  getValidUrlsCount,
} = publishFlow

const handleFetchLogProgressClose = () => {
  handleLogProgressClose()
  fetchFlowErrorMessage.value = ''
}

const showCompleteButton = computed(() => props.showCompleteButton)

provide(crossSeedPanelContextKey, {
  steps,
  activeStep,
  activeTab,
  torrentData,
  reverseMappings,
  filteredTitleComponents,
  initialTitleComponents,
  unrecognizedValue,
  invalidStandardParams,
  reparseTitle,
  isReparsing,
  handleTeamInput,
  allTagOptions,
  invalidTagsList,
  isRestrictedTag,
  getTagType,
  handleTagClose,
  refreshPosters,
  isRefreshingPosters,
  posterImages,
  getProxyImageUrl,
  handleImageErrorWithProxy,
  refreshScreenshots,
  isRefreshingScreenshots,
  confirmScreenshotReview,
  isConfirmingScreenshotReview,
  isScreenshotReviewPending,
  screenshotImages,
  refreshIntro,
  isRefreshingIntro,
  refreshMediainfo,
  isRefreshingMediainfo,
  bdinfoProgress,
  discSize,
  formatFileSize,
  runInBackground,
  stopBDInfoSSE,
  filteredDeclarationsCount,
  filteredDeclarationsList,
  getMappedValue,
  getMappedTags,
  filteredTags,
  parseBBCode,
  autoUpdateExistingTorrent,
  autoAddExistingToDownloader,
  saveAutoAddExistingSetting,
  saveAutoUpdateExistingTorrentSetting,
  isUbitsDisabled,
  allSitesStatus,
  selectedTargetSites,
  selectAllTargetSites,
  clearAllTargetSites,
  getButtonType,
  isTargetSiteSelectable,
  toggleSiteSelection,
  isAutoUpdateHighlightSite,
  isIloliconSite,
  isCurrentSeedAnimationRelated,
  publishProgress,
  downloaderProgress,
  limitAlert,
  groupedResults,
  showSiteLog,
  filterUploadedParam,
  hasValidUrlsInRow,
  openAllSitesInRow,
  getValidUrlsCount,
  showCompleteButton,
  isLoading,
  isEnqueueing,
  isNextButtonDisabled,
  nextButtonTooltipContent,
  isScrolledToBottom,
  handleCancelClick,
  goToPublishPreviewStep,
  handlePreviousStep,
  handleCompleteClick,
  handleScrollOrNextStep,
  handleEnqueue,
  handlePublish,
} satisfies CrossSeedPanelContext)
</script>
