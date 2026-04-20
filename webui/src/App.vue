<!-- src/App.vue -->
<template>
  <div
    v-if="showDesktopCompactTitleBar"
    class="desktop-titlebar glass-nav desktop-drag-region"
    @dblclick="handleWindowMaximiseToggle"
  >
    <div class="desktop-titlebar__brand">
      <img src="/favicon.ico" alt="Logo" height="24" class="brand-logo desktop-titlebar__logo" />
      <div class="desktop-titlebar__text">
        <span class="desktop-titlebar__title">PT Nexus</span>
        <span class="desktop-titlebar__subtitle">{{ desktopCompactSubtitle }}</span>
      </div>
    </div>
    <div class="desktop-titlebar__spacer" />
    <DesktopWindowControls
      class="desktop-titlebar__controls desktop-no-drag"
      compact
      :is-maximised="isMaximised"
      @minimise="minimiseWindow"
      @toggle-maximise="handleWindowMaximiseToggle"
      @close="hideWindowToTray"
    />
  </div>

  <div v-if="!isLoginPage" class="app-nav-wrapper">
    <div v-if="!showMobileNav" class="desktop-main-nav">
      <el-menu
        :default-active="activeRoute"
        class="main-nav glass-nav desktop-drag-region"
        mode="horizontal"
        :ellipsis="false"
        router
        @dblclick="handleDesktopNavDoubleClick"
      >
        <div class="brand-block desktop-no-drag">
          <img src="/favicon.ico" alt="Logo" height="32" class="brand-logo" />
          PT Nexus
        </div>
        <el-menu-item index="/">首页</el-menu-item>
        <el-menu-item index="/info">流量统计</el-menu-item>
        <el-menu-item index="/torrents">一种多站</el-menu-item>
        <el-menu-item index="/data">一站多种</el-menu-item>
        <el-menu-item index="/publish-logs">发种日志</el-menu-item>
        <el-menu-item index="/sites">做种检索</el-menu-item>
        <el-menu-item index="/settings">设置</el-menu-item>
      </el-menu>
      <div
        class="right-buttons-container desktop-no-drag"
        :class="{ 'right-buttons-container--with-window-controls': showDesktopWindowControlsInNav }"
      >
        <el-popover
          placement="bottom-end"
          :width="360"
          trigger="click"
          popper-class="task-monitor-popover"
        >
          <template #reference>
            <el-badge :value="taskMonitorBadge" :hidden="taskMonitorBadge <= 0" :max="99">
              <el-button plain class="task-monitor-button">
                任务
                <span class="task-monitor-button__summary">
                  {{ runningCount }} 运行中 / {{ failedCount }} 失败
                </span>
              </el-button>
            </el-badge>
          </template>
          <div class="task-monitor-panel">
            <div class="task-monitor-panel__header">
              <span>全局任务监控</span>
              <el-button link @click="clearFinishedTasks">清理已结束</el-button>
            </div>
            <div v-if="taskList.length === 0" class="task-monitor-panel__empty">暂无后台任务</div>
            <div v-else class="task-monitor-list">
              <div v-for="task in taskList" :key="task.key" class="task-monitor-item">
                <div class="task-monitor-item__top">
                  <span class="task-monitor-item__title">{{ task.title }}</span>
                  <el-tag :type="taskStatusTagType(task.status)" size="small">{{
                    taskStatusText(task.status)
                  }}</el-tag>
                </div>
                <div v-if="task.progressText" class="task-monitor-item__progress">
                  {{ task.progressText }}
                </div>
                <div v-if="task.message" class="task-monitor-item__message">{{ task.message }}</div>
                <div v-if="task.error" class="task-monitor-item__error">{{ task.error }}</div>
                <div class="task-monitor-item__footer">
                  <span>{{ formatTaskTime(task.updatedAt) }}</span>
                  <el-button
                    v-if="task.routeTarget"
                    link
                    type="primary"
                    @click="openTaskRoute(task.routeTarget)"
                  >
                    查看详情
                  </el-button>
                </div>
              </div>
            </div>
          </div>
        </el-popover>
        <el-link
          href="https://ptn-wiki.sqing33.dpdns.org"
          target="_blank"
          :underline="false"
          class="desktop-link-action"
        >
          <el-icon><Link /></el-icon>
          Wiki
        </el-link>
        <el-link
          href="https://github.com/jadylc/PTNexus"
          target="_blank"
          :underline="false"
          class="desktop-link-action"
        >
          <el-icon><Link /></el-icon>
          GitHub
        </el-link>
        <el-tag size="small" class="version-tag" @click="showVersionDialog">{{
          currentVersion
        }}</el-tag>
        <el-button
          type="primary"
          plain
          :icon="Download"
          :loading="exportingLogs"
          @click="handleExportLogs"
        >
          导出后端日志
        </el-button>
        <el-link
          href="https://github.com/jadylc/PTNexus/issues"
          target="_blank"
          :underline="false"
        >
          <el-button type="primary" plain>反馈</el-button>
        </el-link>
        <el-button
          type="success"
          @click="handleGlobalRefresh"
          :loading="isRefreshing"
          :disabled="isRefreshing"
          plain
        >
          刷新
        </el-button>
      </div>
      <DesktopWindowControls
        v-if="showDesktopWindowControlsInNav"
        class="main-nav__window-controls desktop-no-drag"
        :is-maximised="isMaximised"
        @minimise="minimiseWindow"
        @toggle-maximise="handleWindowMaximiseToggle"
        @close="hideWindowToTray"
      />
    </div>

    <div v-else class="mobile-nav glass-nav">
      <div class="mobile-brand">
        <img src="/favicon.ico" alt="Logo" height="26" class="brand-logo" />
        <div class="mobile-brand-text">
          <span class="mobile-brand-title">PT Nexus</span>
          <span class="mobile-brand-subtitle">{{ currentRouteTitle }}</span>
        </div>
      </div>
      <div class="mobile-top-actions">
        <el-badge :value="taskMonitorBadge" :hidden="taskMonitorBadge <= 0" :max="99">
          <el-button plain size="small" @click="mobileTaskMonitorVisible = true">任务</el-button>
        </el-badge>
        <el-button
          type="success"
          plain
          @click="handleGlobalRefresh"
          :loading="isRefreshing"
          :disabled="isRefreshing"
          size="small"
        >
          刷新
        </el-button>
        <el-button type="primary" plain :icon="Menu" @click="mobileMenuVisible = true" size="small">
          菜单
        </el-button>
      </div>
    </div>

    <el-drawer
      v-model="mobileMenuVisible"
      direction="ltr"
      size="82%"
      :with-header="false"
      append-to-body
      class="mobile-nav-drawer"
    >
      <div class="drawer-header">
        <div class="drawer-title">PT Nexus 导航</div>
        <el-tag size="small" style="cursor: pointer" @click="showVersionDialog">{{
          currentVersion
        }}</el-tag>
      </div>

      <el-menu
        :default-active="activeRoute"
        class="mobile-nav-menu"
        router
        @select="mobileMenuVisible = false"
      >
        <el-menu-item index="/">首页</el-menu-item>
        <el-menu-item index="/info">流量统计</el-menu-item>
        <el-menu-item index="/torrents">一种多站</el-menu-item>
        <el-menu-item index="/data">一站多种</el-menu-item>
        <el-menu-item index="/publish-logs">发种日志</el-menu-item>
        <el-menu-item index="/sites">做种检索</el-menu-item>
        <el-menu-item index="/settings">设置</el-menu-item>
        <el-menu-item index="/settings/general" class="mobile-settings-sub-item">
          基础设置
        </el-menu-item>
        <el-menu-item index="/settings/downloader" class="mobile-settings-sub-item">
          下载器设置
        </el-menu-item>
        <el-menu-item index="/settings/cookie" class="mobile-settings-sub-item">
          站点管理
        </el-menu-item>
      </el-menu>

      <div class="drawer-actions">
        <el-button
          type="primary"
          plain
          :icon="Download"
          :loading="exportingLogs"
          @click="handleExportLogs"
        >
          导出后端日志
        </el-button>
        <el-button
          type="success"
          plain
          @click="handleGlobalRefresh"
          :loading="isRefreshing"
          :disabled="isRefreshing"
        >
          刷新缓存
        </el-button>
        <el-link href="https://ptn-wiki.sqing33.dpdns.org" target="_blank" :underline="false">
          <el-icon><Link /></el-icon>
          Wiki
        </el-link>
        <el-link href="https://github.com/jadylc/PTNexus" target="_blank" :underline="false">
          <el-icon><Link /></el-icon>
          GitHub
        </el-link>
        <el-link
          href="https://github.com/jadylc/PTNexus/issues"
          target="_blank"
          :underline="false"
        >
          <el-button type="primary" plain>反馈</el-button>
        </el-link>
      </div>
    </el-drawer>
    <el-drawer
      v-model="mobileTaskMonitorVisible"
      title="全局任务监控"
      size="88%"
      append-to-body
    >
      <div class="task-monitor-panel task-monitor-panel--mobile">
        <div class="task-monitor-panel__header">
          <span>后台任务</span>
          <el-button link @click="clearFinishedTasks">清理已结束</el-button>
        </div>
        <div v-if="taskList.length === 0" class="task-monitor-panel__empty">暂无后台任务</div>
        <div v-else class="task-monitor-list">
          <div v-for="task in taskList" :key="task.key" class="task-monitor-item">
            <div class="task-monitor-item__top">
              <span class="task-monitor-item__title">{{ task.title }}</span>
              <el-tag :type="taskStatusTagType(task.status)" size="small">{{
                taskStatusText(task.status)
              }}</el-tag>
            </div>
            <div v-if="task.progressText" class="task-monitor-item__progress">{{ task.progressText }}</div>
            <div v-if="task.message" class="task-monitor-item__message">{{ task.message }}</div>
            <div v-if="task.error" class="task-monitor-item__error">{{ task.error }}</div>
            <div class="task-monitor-item__footer">
              <span>{{ formatTaskTime(task.updatedAt) }}</span>
              <el-button
                v-if="task.routeTarget"
                link
                type="primary"
                @click="openTaskRoute(task.routeTarget)"
              >
                查看详情
              </el-button>
            </div>
          </div>
        </div>
      </div>
    </el-drawer>
  </div>

  <main
    :class="[
      'main-content',
      isLoginPage ? 'no-nav' : '',
      showMobileNav && !isLoginPage ? 'mobile-nav-content' : '',
    ]"
  >
    <router-view v-slot="{ Component }">
      <component :is="Component" @ready="handleComponentReady" />
    </router-view>
  </main>

  <!-- Version Update Component (outside navigation bar) -->
  <VersionUpdate @version-loaded="(version) => (currentVersion = version)" ref="versionUpdateRef" />
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Download, Link, Menu } from '@element-plus/icons-vue'
import axios from 'axios'
import DesktopWindowControls from '@/components/desktop/DesktopWindowControls.vue'
import { useDesktopWindowControls } from '@/desktop/windowControls'
import VersionUpdate from '@/components/VersionUpdate.vue'
import { useTaskMonitorStore, type TaskMonitorStatus } from '@/stores/taskMonitor'
import { ElMessage } from '@/utils/uiNotify'

const route = useRoute()
const router = useRouter()
const taskMonitorStore = useTaskMonitorStore()
const {
  hideWindowToTray,
  isDesktopShell,
  isMaximised,
  isWindowsDesktop,
  minimiseWindow,
  toggleWindowMaximise,
} = useDesktopWindowControls()

// 背景图片
const backgroundUrl = ref('https://pic.pting.club/i/2025/10/07/68e4fbfe9be93.jpg')

// 版本信息
const currentVersion = ref('加载中...')

const mobileMenuVisible = ref(false)
const mobileTaskMonitorVisible = ref(false)
const isMobile = ref(false)
const MOBILE_BREAKPOINT = 768

const routeTitleMap: Record<string, string> = {
  '/': '首页',
  '/info': '流量统计',
  '/torrents': '一种多站',
  '/data': '一站多种',
  '/publish-logs': '发种日志',
  '/sites': '做种检索',
  '/settings': '设置',
  '/seed-maintenance': '维护种子信息',
}

const isLoginPage = computed(() => route.path === '/login')

const activeRoute = computed(() => {
  if (route.matched.length > 0) {
    return route.matched[0].path
  }
  return route.path
})

const currentRouteTitle = computed(() => routeTitleMap[activeRoute.value] || '导航')
const desktopCompactSubtitle = computed(() =>
  isLoginPage.value ? '登录' : currentRouteTitle.value,
)
const showMobileNav = computed(() => isMobile.value && !isDesktopShell.value)
const showDesktopCompactTitleBar = computed(() => isWindowsDesktop.value && isLoginPage.value)
const showDesktopWindowControlsInNav = computed(() => isWindowsDesktop.value && !isLoginPage.value)

const taskList = computed(() => taskMonitorStore.taskList)
const runningCount = computed(() => taskMonitorStore.runningCount)
const failedCount = computed(() => taskMonitorStore.failedCount)
const taskMonitorBadge = computed(() => runningCount.value + failedCount.value)

const taskStatusText = (status: TaskMonitorStatus) => {
  if (status === 'running') return '运行中'
  if (status === 'failed') return '失败'
  return '已完成'
}

const taskStatusTagType = (status: TaskMonitorStatus): 'primary' | 'success' | 'danger' => {
  if (status === 'running') return 'primary'
  if (status === 'failed') return 'danger'
  return 'success'
}

const formatTaskTime = (timestamp: number) => {
  if (!timestamp) return '--'
  return new Date(timestamp).toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

const clearFinishedTasks = () => {
  taskMonitorStore.clearFinished()
}

const openTaskRoute = (routeTarget?: { path: string; query?: Record<string, string> }) => {
  if (!routeTarget?.path) return
  mobileTaskMonitorVisible.value = false
  void router.push({ path: routeTarget.path, query: routeTarget.query })
}

const isRefreshing = ref(false)
const exportingLogs = ref(false)

const isRefreshSupportedRoute = (path: string) => {
  return (
    path.startsWith('/torrents') ||
    path.startsWith('/sites') ||
    path.startsWith('/data') ||
    path.startsWith('/seed-maintenance') ||
    path.startsWith('/publish-logs') ||
    path.startsWith('/batch-fetch')
  )
}

// VersionUpdate组件引用
const versionUpdateRef = ref()

const activeComponentRefresher = ref<(() => Promise<void>) | null>(null)

const handleComponentReady = (refreshMethod: () => Promise<void>) => {
  activeComponentRefresher.value = refreshMethod
}

const shouldDelegateRefreshToComponent = (path: string) => {
  return path.startsWith('/torrents') || path.startsWith('/seed-maintenance') || path.startsWith('/publish-logs')
}

const handleGlobalRefresh = async () => {
  if (isRefreshing.value) return

  if (!isRefreshSupportedRoute(route.path)) {
    ElMessage.warning('当前页面不支持刷新操作。')
    return
  }

  isRefreshing.value = true
  ElMessage.info('后台正在刷新缓存...')

  try {
    if (shouldDelegateRefreshToComponent(route.path) && activeComponentRefresher.value) {
      await activeComponentRefresher.value()
    } else {
      await axios.post('/api/refresh_data')
      if (activeComponentRefresher.value) {
        await activeComponentRefresher.value()
      }
    }

    ElMessage.success('数据已刷新！')
  } catch (error: unknown) {
    const message = axios.isAxiosError(error)
      ? (error.response?.data as { message?: string } | undefined)?.message || error.message
      : error instanceof Error
        ? error.message
        : '数据更新失败'
    ElMessage.error(message || '数据更新失败')
  } finally {
    isRefreshing.value = false
  }
}

// handleExportLogs 调用后端导出接口并触发浏览器下载日志文件。
const handleExportLogs = async () => {
  if (exportingLogs.value) {
    return
  }
  exportingLogs.value = true
  try {
    const response = await axios.get('/api/logs/export', {
      responseType: 'blob',
      timeout: 120000,
    })

    const contentType = response.headers['content-type'] || 'text/plain; charset=utf-8'
    const blob = new Blob([response.data], { type: contentType })
    const fileName = parseFileName(response.headers['content-disposition'])

    const fileUrl = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = fileUrl
    link.download = fileName
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(fileUrl)

    ElMessage.success('最近24小时日志导出成功，请将文件发送给维护者')
  } catch (error) {
    const message = await resolveExportErrorMessage(error)
    ElMessage.error(`日志导出失败：${message}`)
  } finally {
    exportingLogs.value = false
  }
}

// parseFileName 从响应头中提取导出文件名，失败时回退默认名称。
const parseFileName = (contentDisposition?: string) => {
  const fallback = 'ptnexus-go-日志-24h.txt'
  if (!contentDisposition || typeof contentDisposition !== 'string') {
    return fallback
  }

  const utf8Match = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1])
    } catch {
      return utf8Match[1]
    }
  }

  const normalMatch = contentDisposition.match(/filename="?([^";]+)"?/i)
  if (normalMatch?.[1]) {
    return normalMatch[1]
  }
  return fallback
}

// resolveExportErrorMessage 兼容 blob/json 文本，提取后端返回的中文错误原因。
const resolveExportErrorMessage = async (error: unknown) => {
  if (axios.isAxiosError(error)) {
    const data = error.response?.data

    if (data instanceof Blob) {
      try {
        const text = await data.text()
        if (!text) return '未知错误'
        try {
          const payload: unknown = JSON.parse(text)
          if (
            typeof payload === 'object' &&
            payload !== null &&
            'message' in payload &&
            typeof payload.message === 'string' &&
            payload.message.trim()
          ) {
            return payload.message
          }
        } catch {
          return text
        }
        return text
      } catch {
        return '无法读取错误详情'
      }
    }

    if (
      typeof data === 'object' &&
      data !== null &&
      'message' in data &&
      typeof data.message === 'string' &&
      data.message.trim()
    ) {
      return data.message
    }

    return error.message || '未知错误'
  }

  if (error instanceof Error && error.message.trim()) {
    return error.message
  }
  return '未知错误'
}

const handleRefreshLoadingChange = (event: Event) => {
  const customEvent = event as CustomEvent<{ refreshing?: boolean }>
  isRefreshing.value = !!customEvent.detail?.refreshing
}

// 加载背景设置
const loadBackgroundSettings = async () => {
  try {
    const response = await axios.get('/api/settings')
    if (response.data?.ui_settings?.background_url) {
      backgroundUrl.value = response.data.ui_settings.background_url
      updateBackground(backgroundUrl.value)
    }
  } catch (error) {
    console.error('加载背景设置失败:', error)
  }
}

// 更新背景图片
const updateBackground = (url: string) => {
  const appElement = document.getElementById('app')
  if (appElement) {
    if (url) {
      appElement.style.backgroundImage = `url('${url}')`
    } else {
      appElement.style.backgroundImage = `url('${backgroundUrl.value}')`
    }
  }
}

// 监听背景更新事件
type BackgroundUpdateDetail = { backgroundUrl: string }
const handleBackgroundUpdate = (event: Event) => {
  const customEvent = event as CustomEvent<BackgroundUpdateDetail>
  const newUrl = customEvent.detail?.backgroundUrl
  if (typeof newUrl !== 'string') return
  backgroundUrl.value = newUrl
  updateBackground(newUrl)
}

// 显示版本更新对话框
const showVersionDialog = () => {
  if (versionUpdateRef.value) {
    versionUpdateRef.value.show()
  }
}

const handleWindowMaximiseToggle = () => {
  void toggleWindowMaximise()
}

const handleDesktopNavDoubleClick = (event: MouseEvent) => {
  if (!showDesktopWindowControlsInNav.value) {
    return
  }

  if (event.target !== event.currentTarget) {
    return
  }

  handleWindowMaximiseToggle()
}

const updateIsMobile = () => {
  isMobile.value = window.innerWidth <= MOBILE_BREAKPOINT
}

watch(
  () => route.path,
  () => {
    mobileMenuVisible.value = false
    mobileTaskMonitorVisible.value = false
  },
)

onMounted(() => {
  updateIsMobile()
  loadBackgroundSettings()
  window.addEventListener('background-updated', handleBackgroundUpdate)
  window.addEventListener('app-global-refresh-loading', handleRefreshLoadingChange as EventListener)
  window.addEventListener('resize', updateIsMobile)
})

onUnmounted(() => {
  window.removeEventListener('background-updated', handleBackgroundUpdate)
  window.removeEventListener('resize', updateIsMobile)
  window.removeEventListener(
    'app-global-refresh-loading',
    handleRefreshLoadingChange as EventListener,
  )
})
</script>

<style>
#app {
  display: flex;
  flex-direction: column;
  height: 100vh;
  min-height: 100vh;
  overflow: hidden;
  position: relative;
  background-image: url('https://pic.pting.club/i/2025/10/07/68e4fbfe9be93.jpg');
  background-size: cover;
  background-position: center;
  background-attachment: fixed;
}

#app::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: rgba(255, 255, 255, 0.5);
  pointer-events: none;
  z-index: 0;
}

body {
  margin: 0;
  padding: 0;
}

/* 禁止 Element Plus 对话框滚动 */
.el-overlay-dialog {
  display: flex;
  justify-content: center;
  align-items: center;
  overflow: hidden;
}

.el-overlay-dialog .el-dialog {
  margin: 0;
}
</style>

<style scoped>
.app-nav-wrapper {
  flex-shrink: 0;
  position: relative;
  z-index: 1;
}

.desktop-main-nav {
  position: relative;
  flex-shrink: 0;
  z-index: 1;
}

.main-nav {
  border-bottom: solid 1px var(--el-menu-border-color);
  flex-shrink: 0;
  height: 40px;
  display: flex;
  align-items: center;
  position: relative;
  z-index: 1;
}

.main-nav :deep(.el-menu-item),
.main-nav :deep(.el-sub-menu) {
  height: 100%;
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  --wails-draggable: no-drag;
}

.brand-block {
  padding: 5px 15px;
  line-height: 32px;
  display: flex;
  align-items: center;
}

.brand-logo {
  margin-right: 8px;
  vertical-align: middle;
}

.right-buttons-container {
  position: absolute;
  right: 20px;
  top: 3px;
  display: flex;
  align-items: center;
  gap: 10px;
  z-index: 2;
}

.right-buttons-container--with-window-controls {
  right: 150px;
}

.desktop-link-action {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.version-tag {
  cursor: pointer;
}

.main-nav__window-controls {
  position: absolute;
  top: 0;
  right: 0;
  height: 100%;
  z-index: 3;
}

.desktop-titlebar {
  flex-shrink: 0;
  height: 38px;
  display: flex;
  align-items: stretch;
  border-bottom: 1px solid rgba(209, 218, 229, 0.8);
  position: relative;
  z-index: 1;
}

.desktop-titlebar__brand {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 0 12px;
}

.desktop-titlebar__logo {
  margin-right: 0;
}

.desktop-titlebar__text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.05;
}

.desktop-titlebar__title {
  font-size: 13px;
  font-weight: 600;
  color: #243347;
}

.desktop-titlebar__subtitle {
  font-size: 11px;
  color: #6f8094;
}

.desktop-titlebar__spacer {
  flex: 1 1 auto;
  min-width: 0;
}

.desktop-titlebar__controls {
  margin-left: auto;
}

.desktop-drag-region {
  --wails-draggable: drag;
}

.desktop-no-drag {
  --wails-draggable: no-drag;
}

.mobile-nav {
  height: 52px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  border-bottom: 1px solid var(--el-menu-border-color);
  flex-shrink: 0;
  z-index: 1;
}

.mobile-brand {
  display: flex;
  align-items: center;
  min-width: 0;
}

.mobile-brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.1;
  min-width: 0;
}

.mobile-brand-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
}

.mobile-brand-subtitle {
  font-size: 12px;
  color: #606266;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mobile-top-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.mobile-nav-content {
  overflow: auto;
}

.drawer-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.drawer-title {
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}

.mobile-nav-menu {
  border-right: none;
  margin-bottom: 16px;
}

.mobile-nav-menu :deep(.mobile-settings-sub-item) {
  padding-left: 38px !important;
  color: #606266;
  font-size: 13px;
}

.drawer-actions {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.drawer-actions :deep(.el-button + .el-button) {
  margin-left: 0 !important;
}

.drawer-actions .el-link {
  width: fit-content;
}

.task-monitor-button {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.task-monitor-button__summary {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.task-monitor-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.task-monitor-panel--mobile {
  min-height: 100%;
}

.task-monitor-panel__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-weight: 600;
}

.task-monitor-panel__empty {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  padding: 8px 0;
}

.task-monitor-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 420px;
  overflow: auto;
  padding-right: 4px;
}

.task-monitor-panel--mobile .task-monitor-list {
  max-height: none;
}

.task-monitor-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px solid rgba(148, 163, 184, 0.18);
}

.task-monitor-item__top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.task-monitor-item__title {
  color: var(--el-text-color-primary);
  font-weight: 600;
  line-height: 1.4;
}

.task-monitor-item__progress,
.task-monitor-item__message,
.task-monitor-item__footer {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.task-monitor-item__error {
  color: var(--el-color-danger);
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.task-monitor-item__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.task-monitor-item__footer span {
  min-width: 0;
}

.main-content {
  flex: 1 1 auto;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  min-height: 0;
  position: relative;
  z-index: 1;
}

.main-content.no-nav {
  overflow: auto;
}

@media (max-width: 768px) {
  #app {
    background-attachment: scroll;
  }

  .main-content {
    overflow: auto;
  }
}
</style>
