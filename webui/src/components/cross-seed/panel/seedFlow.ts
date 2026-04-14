import { computed, nextTick, type ComputedRef, type Ref } from 'vue'
import axios from 'axios'
import { ElNotification } from '@/utils/uiNotify'
import { resolveSourceTorrentId } from '@/utils/sourceTorrentId'

import type {
  ReverseMappings,
  ScreenshotReviewStatus,
  SiteStatus,
  StandardParamKey,
  TagOption,
  TorrentData,
  TitleComponent,
} from '../crossSeedPanelContext'
import type { DownloaderListItem, WorkingTorrent } from './types'

export type SeedFlowDeps = {
  sourceSite: ComputedRef<string>
  sourceTorrentId: ComputedRef<string>
  torrent: ComputedRef<WorkingTorrent | null>
  crossSeedStore: { setParams: (params: object) => void }

  allSitesStatus: Ref<SiteStatus[]>
  selectedTargetSites: Ref<string[]>
  downloaderList: Ref<DownloaderListItem[]>

  autoAddExistingToDownloader: Ref<boolean>
  autoUpdateExistingTorrent: Ref<boolean>

  activeStep: Ref<number>
  isLoading: Ref<boolean>
  isDataFromDatabase: Ref<boolean>
  taskId: Ref<string | null>

  torrentData: Ref<TorrentData>
  reverseMappings: Ref<ReverseMappings>

  logProgressTaskId: Ref<string>
  showLogProgress: Ref<boolean>

  fetchFlowErrorMessage: Ref<string>

  filterExtraEmptyLines: (text: string) => string
  normalizeIntroBodyAndMediainfo: (rawBody: string, rawMediainfo: string) => {
    body: string
    mediainfo: string
  }

  checkAndStartBDInfoProgress: (seedId: string, isFromFetch?: boolean) => void
  openFetchedScreenshotPreview: () => Promise<void>
  handleApiError: (error: unknown, defaultMessage: string) => void
  isTargetSiteSelectable: (siteName: string) => boolean

  screenshotValid: Ref<boolean>
  screenshotImages: ComputedRef<string[]>
}

export type SeedFlowApi = {
  getEnglishSiteName: (chineseSiteName: string) => Promise<string>
  fetchSitesStatus: () => Promise<void>
  fetchCrossSeedSettings: () => Promise<void>
  saveAutoAddExistingSetting: () => Promise<void>
  saveAutoUpdateExistingTorrentSetting: () => Promise<void>
  fetchTorrentInfo: (prefetchedDbSeedInfo?: unknown) => Promise<void>
  handleTeamInput: (param: TitleComponent, value: string) => void
  goToPublishPreviewStep: () => Promise<void>
  goToSelectSiteStep: () => Promise<void>
  toggleSiteSelection: (siteName: string) => void
  selectAllTargetSites: () => void
  clearAllTargetSites: () => void
  invalidStandardParams: ComputedRef<Array<StandardParamKey | 'tags'>>
  allTagOptions: ComputedRef<TagOption[]>
}

type DbSeedRecord = {
  hash?: string
  title?: string
  title_components?: TitleComponent[]
  subtitle?: string
  imdb_link?: string
  douban_link?: string
  tmdb_link?: string
  screenshot_review_status?: string
  statement?: string
  poster?: string
  body?: string
  mediainfo?: string
  screenshots?: string
  removed_ardtudeclarations?: string[]
  source_params?: Record<string, unknown>
  standardized_params?: Record<string, unknown>
  type?: string
  medium?: string
  video_codec?: string
  audio_codec?: string
  resolution?: string
  team?: string
  source?: string
  tags?: string[]
  final_publish_parameters?: Record<string, string>
  complete_publish_params?: Record<string, unknown>
  raw_params_for_preview?: Record<string, unknown>
}

type DbSeedInfoResponse = {
  should_fetch?: boolean
  task_id?: string
  success?: boolean
  reverse_mappings?: ReverseMappings
  data?: DbSeedRecord
}

export function createSeedFlow(deps: SeedFlowDeps): SeedFlowApi {
  const {
    sourceSite,
    sourceTorrentId,
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
  } = deps

  const normalizeScreenshotReviewStatus = (value: unknown): ScreenshotReviewStatus => {
    if (typeof value !== 'string') return 'none'
    const normalized = value.trim().toLowerCase()
    if (normalized === 'pending' || normalized === 'confirmed') {
      return normalized
    }
    return 'none'
  }

  const checkScreenshotValidity = async () => {
    const screenshots = screenshotImages.value
    if (screenshots.length === 0) {
      screenshotValid.value = true
      return
    }

    let allValid = true
    for (const url of screenshots) {
      try {
        await new Promise((resolve, reject) => {
          const img = new Image()
          img.onload = () => resolve(true)
          img.onerror = () => reject(new Error('Image load failed'))
          img.src = url
        })
      } catch {
        allValid = false
        break
      }
    }

    screenshotValid.value = allValid
  }

  const setFetchFlowError = (message: string) => {
    fetchFlowErrorMessage.value = message.trim() || '获取种子信息时发生错误，请查看后台日志。'
    showLogProgress.value = true
  }

  const getEnglishSiteName = async (chineseSiteName: string): Promise<string> => {
    // 优先从当前种子携带的站点信息中读取，避免重复请求 /api/sites/status。
    const currentTorrent = torrent.value
    const currentSiteDetails = currentTorrent?.sites?.[chineseSiteName] as
      | { site?: unknown; site_name?: unknown }
      | undefined
    const directSite =
      (typeof currentSiteDetails?.site === 'string' ? currentSiteDetails.site.trim() : '') ||
      (typeof currentSiteDetails?.site_name === 'string'
        ? currentSiteDetails.site_name.trim()
        : '')
    if (directSite) {
      return directSite
    }

    // 首先尝试从已加载的 allSitesStatus 中获取
    const siteInfo = allSitesStatus.value.find((s) => s.name === chineseSiteName)
    if (siteInfo?.site) {
      return siteInfo.site
    }

    // 如果 allSitesStatus 还没有加载，直接调用接口获取站点信息
    try {
      const response = await axios.get('/api/sites/status')
      allSitesStatus.value = response.data

      // 再次尝试从更新的 allSitesStatus 中获取
      const updatedSiteInfo = allSitesStatus.value.find((s) => s.name === chineseSiteName)
      if (updatedSiteInfo?.site) {
        return updatedSiteInfo.site
      }
    } catch (error) {
      console.warn('获取站点状态失败:', error)
    }

    return chineseSiteName.toLowerCase()
  }

  const fetchSitesStatus = async () => {
    try {
      const response = await axios.get('/api/sites/status')
      allSitesStatus.value = response.data
      const downloaderResponse = await axios.get('/api/downloaders_list')
      downloaderList.value = downloaderResponse.data
    } catch (error: unknown) {
      console.warn('获取站点状态列表或下载器列表失败:', error)
      ElNotification.error({ title: '错误', message: '无法从服务器获取站点状态列表或下载器列表' })
    }
  }

  const fetchCrossSeedSettings = async () => {
    try {
      const response = await axios.get('/api/settings/cross_seed')
      autoAddExistingToDownloader.value = !!response.data?.auto_add_existing_to_downloader
      autoUpdateExistingTorrent.value = !!response.data?.auto_update_existing_torrent
    } catch (error) {
      console.warn('获取发种设置失败:', error)
    }
  }

  const saveAutoAddExistingSetting = async () => {
    try {
      await axios.post('/api/settings/cross_seed', {
        auto_add_existing_to_downloader: autoAddExistingToDownloader.value,
      })
    } catch (error) {
      console.warn('保存发种设置失败:', error)
      ElNotification.error({ title: '错误', message: '保存设置失败' })
    }
  }

  const saveAutoUpdateExistingTorrentSetting = async () => {
    try {
      await axios.post('/api/settings/cross_seed', {
        auto_update_existing_torrent: autoUpdateExistingTorrent.value,
      })
    } catch (error) {
      console.warn('保存发种设置失败:', error)
      ElNotification.error({ title: '错误', message: '保存设置失败' })
    }
  }

  const fetchTorrentInfo = async (prefetchedDbSeedInfo?: unknown) => {
    if (!sourceSite.value || !torrent.value) return
    fetchFlowErrorMessage.value = ''

    const siteDetails = torrent.value.sites[sourceSite.value]
    if (!siteDetails) {
      const message = `无法获取源站点 ${sourceSite.value} 的详情信息。`
      ElNotification.error({ title: '参数错误', message, duration: 0, showClose: true })
      setFetchFlowError(message)
      return
    }

    const torrentId = resolveSourceTorrentId({
      sourceInfoTorrentId: sourceTorrentId.value,
      siteDetails,
    })
    if (!torrentId) {
      const message = `无法从源站点 ${sourceSite.value} 的链接或种子ID中提取有效的源站ID。`
      ElNotification.error({ title: '参数错误', message, duration: 0, showClose: true })
      setFetchFlowError(message)
      return
    }

    isLoading.value = true

    // 生成任务ID并显示进度组件
    const tempTaskId = `fetch_${torrentId}_${Date.now()}`
    if (!prefetchedDbSeedInfo) {
      logProgressTaskId.value = tempTaskId
      showLogProgress.value = true
    } else {
      showLogProgress.value = false
    }

    let dbReadErrorMessage: string | null = null
    const englishSiteName = await getEnglishSiteName(sourceSite.value)

    // 步骤1: 尝试从数据库读取种子信息
    try {
      if (!prefetchedDbSeedInfo) {
        console.log(
          `尝试从数据库读取种子信息: ${torrentId} from ${sourceSite.value} (${englishSiteName})`,
        )
      }
      const dbResponse = prefetchedDbSeedInfo
        ? ({ status: 200, data: prefetchedDbSeedInfo as DbSeedInfoResponse })
        : await axios.get<DbSeedInfoResponse>('/api/migrate/get_db_seed_info', {
            params: {
              torrent_id: torrentId,
              site_name: englishSiteName,
              task_id: tempTaskId, // 传递task_id给后端
            },
            timeout: 600000, // 10分钟超时
          })

      // 检查是否需要继续抓取（202状态码）
      if (!prefetchedDbSeedInfo && dbResponse.status === 202 && dbResponse.data.should_fetch) {
        console.log('数据库中没有缓存，继续使用同一日志流从源站点抓取...')
        // 使用返回的task_id继续抓取（不关闭日志流）
        const continuedTaskId = dbResponse.data.task_id || tempTaskId

        // 直接调用 fetch_and_store，传入相同的 task_id
        try {
          const storeResponse = await axios.post(
            '/api/migrate/fetch_and_store',
            {
              sourceSite: sourceSite.value,
              searchTerm: torrentId,
              savePath: torrent.value.save_path,
              torrentName: torrent.value.name,
              downloaderId: torrent.value.downloaderId,
              screenshotReviewMode: 'interactive',
              task_id: continuedTaskId, // 传递相同的task_id以继续使用同一日志流
            },
            {
              timeout: 600000,
            },
          )

          if (!storeResponse.data.success) {
            ElNotification.closeAll()

            // 1. 获取错误消息
            const errorMsg = storeResponse.data.message || '从源站点抓取失败'

            // 2. 在抓取流程里展示错误
            setFetchFlowError(errorMsg)

            // 4. 停止加载，但不触发取消（修复问题：避免组件销毁导致弹窗无法显示）
            isLoading.value = false
            return
          }

          // 抓取成功后，再次从数据库读取（使用相同逻辑）
          const finalDbResponse = await axios.get('/api/migrate/get_db_seed_info', {
            params: {
              torrent_id: torrentId,
              site_name: englishSiteName,
            },
            timeout: 600000,
          })

          if (!finalDbResponse.data.success) {
            ElNotification.closeAll()

            // 1. 获取错误消息
            const errorMsg = '数据抓取成功但从数据库读取失败'

            // 2. 在抓取流程里展示错误
            setFetchFlowError(errorMsg)

            // 4. 停止加载，但不触发取消（修复问题：避免组件销毁导致弹窗无法显示）
            isLoading.value = false
            return
          }

          // 处理成功的数据（与下面的逻辑相同）
          ElNotification.closeAll()
          ElNotification.success({
            title: '抓取成功',
            message: '种子信息已成功抓取并存储到数据库，请核对。',
          })

          const dbData = finalDbResponse.data.data
          if (finalDbResponse.data.reverse_mappings) {
            reverseMappings.value = finalDbResponse.data.reverse_mappings
          }

          // 构建复合主键作为seed_id
          const compositeSeedId = `${dbData.hash || torrentId}_${torrentId}_${englishSiteName}`

          torrentData.value = {
            seed_id: compositeSeedId,
            original_main_title: dbData.title || '',
            title_components: dbData.title_components || [],
            subtitle: dbData.subtitle,
            imdb_link: dbData.imdb_link,
            douban_link: dbData.douban_link,
            tmdb_link: dbData.tmdb_link,
            screenshot_review_status: normalizeScreenshotReviewStatus(
              dbData.screenshot_review_status,
            ),
            intro: {
              statement: filterExtraEmptyLines(dbData.statement) || '',
              poster: dbData.poster || '',
              body: normalizeIntroBodyAndMediainfo(dbData.body, dbData.mediainfo).body || '',
              screenshots: dbData.screenshots || '',
              removed_ardtudeclarations: dbData.removed_ardtudeclarations || [],
            },
            mediainfo: normalizeIntroBodyAndMediainfo(dbData.body, dbData.mediainfo).mediainfo || '',
            source_params: dbData.source_params || {},
            standardized_params: {
              type: dbData.type || '',
              medium: dbData.medium || '',
              video_codec: dbData.video_codec || '',
              audio_codec: dbData.audio_codec || '',
              resolution: dbData.resolution || '',
              team: dbData.team || '',
              source: dbData.source || '',
              tags: (dbData.tags || []).sort((a: string, b: string) => {
                const restricted = ['禁转', 'tag.禁转', '限转', 'tag.限转', '分集', 'tag.分集']
                const isRa = restricted.includes(a)
                const isRb = restricted.includes(b)
                return isRa === isRb ? 0 : isRa ? -1 : 1
              }),
            },
            final_publish_parameters: dbData.final_publish_parameters || {},
            complete_publish_params: dbData.complete_publish_params || {},
            raw_params_for_preview: dbData.raw_params_for_preview || {},
          }

          taskId.value = storeResponse.data.task_id
          isDataFromDatabase.value = true
          activeStep.value = 0

          // 检查 BDInfo 进度状态（从抓取流程调用，增加重试次数和延迟）
          checkAndStartBDInfoProgress(compositeSeedId, true)

          isLoading.value = false
          await nextTick()
          await checkScreenshotValidity()
          if (storeResponse.data.screenshot_preview_required) {
            await openFetchedScreenshotPreview()
          }
          return
        } catch (error: unknown) {
          ElNotification.closeAll()
          handleApiError(error, '从源站点抓取时发生错误，请查看后台日志。')
          isLoading.value = false
          return
        }
      } else if (dbResponse.data.success) {
        ElNotification.closeAll()
        ElNotification.success({
          title: '读取成功',
          message: '种子信息已从数据库成功加载，请核对。',
        })

        // 验证数据库返回的数据完整性
        const dbData = dbResponse.data.data
        if (!dbData || !dbData.title) {
          throw new Error('数据库返回的种子信息不完整')
        }

        // 从后端响应中提取反向映射表
        if (dbResponse.data.reverse_mappings) {
          reverseMappings.value = dbResponse.data.reverse_mappings
          console.log('成功加载反向映射表:', reverseMappings.value)
          console.log('type映射数量:', Object.keys(reverseMappings.value.type || {}).length)
          console.log('当前standardized_params:', dbData.standardized_params)
        } else {
          console.warn('后端未返回反向映射表，将使用空的默认映射')
        }

        // 构建复合主键作为seed_id
        const compositeSeedId = `${dbData.hash || torrentId}_${torrentId}_${englishSiteName}`

        // 从数据库返回的数据中提取相关信息
        torrentData.value = {
          seed_id: compositeSeedId,
          original_main_title: dbData.title || '',
          title_components: dbData.title_components || [],
          subtitle: dbData.subtitle || '',
          imdb_link: dbData.imdb_link || '',
          douban_link: dbData.douban_link || '',
          tmdb_link: dbData.tmdb_link || '',
          screenshot_review_status: normalizeScreenshotReviewStatus(dbData.screenshot_review_status),
          intro: {
            statement: filterExtraEmptyLines(dbData.statement || '') || '',
            poster: dbData.poster || '',
            body: normalizeIntroBodyAndMediainfo(dbData.body || '', dbData.mediainfo || '').body || '',
            screenshots: dbData.screenshots || '',
            removed_ardtudeclarations: dbData.removed_ardtudeclarations || [],
          },
          mediainfo:
            normalizeIntroBodyAndMediainfo(dbData.body || '', dbData.mediainfo || '').mediainfo ||
            '',
          source_params: dbData.source_params || {},
          standardized_params: {
            type: dbData.type || '',
            medium: dbData.medium || '',
            video_codec: dbData.video_codec || '',
            audio_codec: dbData.audio_codec || '',
            resolution: dbData.resolution || '',
            team: dbData.team || '',
            source: dbData.source || '',
            tags: (dbData.tags || []).sort((a: string, b: string) => {
              const restricted = ['禁转', 'tag.禁转', '限转', 'tag.限转', '分集', 'tag.分集']
              const isRa = restricted.includes(a)
              const isRb = restricted.includes(b)
              return isRa === isRb ? 0 : isRa ? -1 : 1
            }),
          },
          final_publish_parameters: dbData.final_publish_parameters || {},
          complete_publish_params: dbData.complete_publish_params || {},
          raw_params_for_preview: dbData.raw_params_for_preview || {},
        }

        // 如果没有解析过的标题组件，自动解析主标题
        if ((!dbData.title_components || dbData.title_components.length === 0) && dbData.title) {
          try {
            const parseResponse = await axios.post('/api/utils/parse_title', { title: dbData.title })
            if (parseResponse.data.success) {
              torrentData.value.title_components = parseResponse.data.components
              ElNotification.info({
                title: '标题解析',
                message: '已自动解析主标题为组件信息。',
              })
            }
          } catch (error) {
            console.warn('自动解析标题失败:', error)
          }
        }

        console.log('设置torrentData.standardized_params:', torrentData.value.standardized_params)
        console.log('检查绑定 - type:', torrentData.value.standardized_params.type)
        console.log('检查绑定 - medium:', torrentData.value.standardized_params.medium)

        // 直接使用从数据库返回的 taskId，如果后端没有返回则生成标识符
        if (dbResponse.data.task_id) {
          taskId.value = dbResponse.data.task_id // 使用从数据库返回的 taskId
          ElNotification.success({
            title: '缓存准备完成',
            message: '发布任务已准备就绪',
          })
        } else {
          // 如果后端未返回task_id，回退到标识符
          taskId.value = `db_${torrentId}_${englishSiteName}`
          console.warn('后端未返回taskId，使用标识符')
        }
        isDataFromDatabase.value = true // Mark that data was loaded from database

        // 检查 BDInfo 进度状态（从数据库读取，使用默认重试设置）
        checkAndStartBDInfoProgress(compositeSeedId, false)

        // 自动提取链接的逻辑保持不变
        if (
          (!torrentData.value.imdb_link || !torrentData.value.douban_link) &&
          torrentData.value.intro.body
        ) {
          let imdbExtracted = false
          let doubanExtracted = false
          if (!torrentData.value.imdb_link) {
            const imdbRegex = /(https?:\/\/www\.imdb\.com\/title\/tt\d+)/
            const imdbMatch = torrentData.value.intro.body.match(imdbRegex)
            if (imdbMatch && imdbMatch[1]) {
              torrentData.value.imdb_link = imdbMatch[1]
              imdbExtracted = true
            }
          }
          if (!torrentData.value.douban_link) {
            const doubanRegex = /(https:\/\/movie\.douban\.com\/subject\/\d+)/
            const doubanMatch = torrentData.value.intro.body.match(doubanRegex)
            if (doubanMatch && doubanMatch[1]) {
              torrentData.value.douban_link = doubanMatch[1]
              doubanExtracted = true
            }
          }
          if (imdbExtracted || doubanExtracted) {
            const messages = []
            if (imdbExtracted) messages.push('IMDb链接')
            if (doubanExtracted) messages.push('豆瓣链接')
            ElNotification.info({
              title: '自动填充',
              message: `已从简介正文中自动提取并填充 ${messages.join(' 和 ')}。`,
            })
          }
        }

        activeStep.value = 0
        // Check screenshot validity after loading data
        nextTick(() => {
          void checkScreenshotValidity()
        })
        // Set flag to indicate data was loaded from database
        isDataFromDatabase.value = true
        // 【修复】在从数据库成功读取后关闭加载动画
        isLoading.value = false
        // Skip the scraping part since we have data from database
        return
      } else {
        // 数据库中不存在该记录，这是正常情况，不需要记录为错误
        console.log('数据库中没有找到种子信息，开始抓取数据...')
      }
    } catch (error: unknown) {
      const message = axios.isAxiosError(error)
        ? ((error.response?.data as { message?: string } | undefined)?.message || error.message)
        : error instanceof Error
          ? error.message
          : String(error)
      dbReadErrorMessage = message
      console.log('从数据库读取失败，开始抓取数据...', error)

      if (axios.isAxiosError(error)) {
        if (error.code === 'ECONNABORTED' || message.includes('timeout')) {
          console.warn('数据库读取超时，将尝试直接抓取数据...')
        } else if ((error.response?.status ?? 0) >= 500) {
          console.warn('数据库服务器错误，将尝试直接抓取数据...')
        } else {
          console.warn('数据库读取发生未知错误，将尝试直接抓取数据...')
        }
      } else {
        console.warn('数据库读取发生未知错误，将尝试直接抓取数据...')
      }
    }

    // 步骤2: 如果数据库中没有数据，则进行抓取和存储
    try {
      ElNotification.closeAll()
      ElNotification({
        title: '正在抓取',
        message: '正在从源站点抓取种子信息并存储到数据库...',
        type: 'info',
        duration: 0,
      })

      const fallbackDownloaderId =
        torrent.value.downloaderIds && torrent.value.downloaderIds.length > 0
          ? torrent.value.downloaderIds[0]
          : null
      const primaryDownloaderId = torrent.value.downloaderId || fallbackDownloaderId

      // 如果有数据库错误，显示警告信息
      if (dbReadErrorMessage) {
        console.warn(`由于数据库读取失败（${dbReadErrorMessage}），正在直接抓取数据...`)
        ElNotification.warning({
          title: '数据库读取失败',
          message: '正在尝试直接抓取数据，请稍候...',
          duration: 3000,
        })
      }

      const storeResponse = await axios.post(
        '/api/migrate/fetch_and_store',
        {
          sourceSite: sourceSite.value,
          searchTerm: torrentId,
          savePath: torrent.value.save_path,
          torrentName: torrent.value.name,
          downloaderId: primaryDownloaderId,
          screenshotReviewMode: 'interactive',
        },
        {
          timeout: 600000, // 10分钟超时，用于抓取和存储
        },
      )

      if (storeResponse.data.success) {
        // 抓取成功后，立即从数据库读取数据
        console.log('数据抓取成功，立即从数据库读取...')
        let dbReadAttempt = 0
        const maxDbReadAttempts = 3
        let dbResponseAfterStore = null

        // 重试机制：多次尝试从数据库读取
        while (dbReadAttempt < maxDbReadAttempts) {
          dbReadAttempt++
          try {
            const retryEnglishSiteName = await getEnglishSiteName(sourceSite.value)
            console.log(
              `重试从数据库读取种子信息: ${torrentId} from ${sourceSite.value} (${retryEnglishSiteName})`,
            )
            dbResponseAfterStore = await axios.get('/api/migrate/get_db_seed_info', {
              params: {
                torrent_id: torrentId,
                site_name: retryEnglishSiteName,
              },
              timeout: 600000, // 10分钟超时
            })

            if (dbResponseAfterStore.data.success) {
              break // 成功读取，退出重试循环
            } else {
              console.warn(`数据库读取第${dbReadAttempt}次失败：${dbResponseAfterStore.data.message}`)
              if (dbReadAttempt < maxDbReadAttempts) {
                await new Promise((resolve) => setTimeout(resolve, 1000)) // 等待1秒后重试
              }
            }
          } catch (readError) {
            console.warn(`数据库读取第${dbReadAttempt}次失败：`, readError)
            if (dbReadAttempt < maxDbReadAttempts) {
              await new Promise((resolve) => setTimeout(resolve, 1000)) // 等待1秒后重试
            } else {
              throw readError // 重试次数用尽，抛出错误
            }
          }
        }

        if (dbResponseAfterStore && dbResponseAfterStore.data.success) {
          ElNotification.closeAll()

          // 验证数据完整性
          const dbData = dbResponseAfterStore.data.data
          if (!dbData || !dbData.title) {
            throw new Error('数据库返回的种子信息不完整')
          }

          // 从后端响应中提取反向映射表
          if (dbResponseAfterStore.data.reverse_mappings) {
            reverseMappings.value = dbResponseAfterStore.data.reverse_mappings
            console.log('成功加载反向映射表:', reverseMappings.value)
          } else {
            console.warn('后端未返回反向映射表，将使用空的默认映射')
          }

          ElNotification.success({
            title: '抓取成功',
            message: dbReadErrorMessage
              ? '种子信息已成功抓取，请核对。由于数据库读取失败，数据未持久化存储。'
              : '种子信息已成功抓取并存储到数据库，请核对。',
          })

          // 构建复合主键作为seed_id
          const compositeSeedId = `${dbData.hash || torrentId}_${torrentId}_${englishSiteName}`

          torrentData.value = {
            seed_id: compositeSeedId,
            original_main_title: dbData.title || '',
            title_components: dbData.title_components || [],
            subtitle: dbData.subtitle,
            imdb_link: dbData.imdb_link,
            douban_link: dbData.douban_link,
            tmdb_link: dbData.tmdb_link,
            screenshot_review_status: normalizeScreenshotReviewStatus(
              dbData.screenshot_review_status,
            ),
            intro: {
              statement: filterExtraEmptyLines(dbData.statement) || '',
              poster: dbData.poster || '',
              body: normalizeIntroBodyAndMediainfo(dbData.body, dbData.mediainfo).body || '',
              screenshots: dbData.screenshots || '',
              removed_ardtudeclarations: dbData.removed_ardtudeclarations || [],
            },
            mediainfo: normalizeIntroBodyAndMediainfo(dbData.body, dbData.mediainfo).mediainfo || '',
            source_params: dbData.source_params || {},
            standardized_params: {
              type: dbData.type || '',
              medium: dbData.medium || '',
              video_codec: dbData.video_codec || '',
              audio_codec: dbData.audio_codec || '',
              resolution: dbData.resolution || '',
              team: dbData.team || '',
              source: dbData.source || '',
              tags: (dbData.tags || []).sort((a: string, b: string) => {
                const restricted = ['禁转', 'tag.禁转', '限转', 'tag.限转', '分集', 'tag.分集']
                const isRa = restricted.includes(a)
                const isRb = restricted.includes(b)
                return isRa === isRb ? 0 : isRa ? -1 : 1
              }),
            },
            final_publish_parameters: dbData.final_publish_parameters || {},
            complete_publish_params: dbData.complete_publish_params || {},
            raw_params_for_preview: dbData.raw_params_for_preview || {},
          }

          // 如果没有解析过的标题组件，自动解析主标题
          if ((!dbData.title_components || dbData.title_components.length === 0) && dbData.title) {
            try {
              const parseResponse = await axios.post('/api/utils/parse_title', {
                title: dbData.title,
              })
              if (parseResponse.data.success) {
                torrentData.value.title_components = parseResponse.data.components
                ElNotification.info({
                  title: '标题解析',
                  message: '已自动解析主标题为组件信息。',
                })
              }
            } catch (error) {
              console.warn('自动解析标题失败:', error)
            }
          }

          taskId.value = storeResponse.data.task_id
          isDataFromDatabase.value = true // Mark that data was loaded from database

          // 自动提取链接的逻辑保持不变
          if (
            (!torrentData.value.imdb_link || !torrentData.value.douban_link) &&
            torrentData.value.intro.body
          ) {
            let imdbExtracted = false
            let doubanExtracted = false
            if (!torrentData.value.imdb_link) {
              const imdbRegex = /(https?:\/\/www\.imdb\.com\/title\/tt\d+)/
              const imdbMatch = torrentData.value.intro.body.match(imdbRegex)
              if (imdbMatch && imdbMatch[1]) {
                torrentData.value.imdb_link = imdbMatch[1]
                imdbExtracted = true
              }
            }
            if (!torrentData.value.douban_link) {
              const doubanRegex = /(https:\/\/movie\.douban\.com\/subject\/\d+)/
              const doubanMatch = torrentData.value.intro.body.match(doubanRegex)
              if (doubanMatch && doubanMatch[1]) {
                torrentData.value.douban_link = doubanMatch[1]
                doubanExtracted = true
              }
            }
            if (imdbExtracted || doubanExtracted) {
              const messages = []
              if (imdbExtracted) messages.push('IMDb链接')
              if (doubanExtracted) messages.push('豆瓣链接')
              ElNotification.info({
                title: '自动填充',
                message: `已从简介正文中自动提取并填充 ${messages.join(' 和 ')}。`,
              })
            }
          }

          activeStep.value = 0
          isLoading.value = false
          await nextTick()
          await checkScreenshotValidity()
          if (storeResponse.data.screenshot_preview_required) {
            await openFetchedScreenshotPreview()
          }
        } else {
          ElNotification.closeAll()

          // 1. 获取错误消息
          const errorMsg = `数据抓取成功但数据库读取失败，已重试${maxDbReadAttempts}次。请检查数据库连接或稍后重试。`

          // 2. 在抓取流程里展示错误
          setFetchFlowError(errorMsg)

          // 4. 停止加载，但不触发取消（修复问题：避免组件销毁导致弹窗无法显示）
          isLoading.value = false
        }
      } else {
        ElNotification.closeAll()
        const errorMessage = storeResponse.data.message || '抓取种子信息失败'

        // 1. 获取错误消息
        let errorMsg = errorMessage

        // 2. 如果是数据库相关的错误，提供更详细的建议
        if (errorMessage.includes('数据库') || dbReadErrorMessage) {
          errorMsg = `${errorMessage}。可能由于数据库连接问题导致，请检查数据库状态。`
        }

        // 3. 在抓取流程里展示错误
        setFetchFlowError(errorMsg)

        // 5. 停止加载，但不触发取消（修复问题：避免组件销毁导致弹窗无法显示）
        isLoading.value = false
      }
    } catch (error: unknown) {
      ElNotification.closeAll()

      const message = axios.isAxiosError(error)
        ? ((error.response?.data as { message?: string } | undefined)?.message || error.message)
        : error instanceof Error
          ? error.message
          : String(error)

      const code = axios.isAxiosError(error) ? error.code : undefined
      const status = axios.isAxiosError(error) ? error.response?.status : undefined

      // 区分不同类型的错误并提供更具体的错误信息
      if (code === 'ECONNABORTED' || message.includes('timeout')) {
        // 1. 获取错误消息
        const msg = '抓取种子信息超时，请检查网络连接或稍后重试。'
        setFetchFlowError(msg)
      } else if (status === 404) {
        // 1. 获取错误消息
        const msg = '在源站点未找到指定的种子，请检查种子ID是否正确。'
        setFetchFlowError(msg)
      } else if (typeof status === 'number' && status >= 500) {
        // 1. 获取错误消息
        const msg = '后端服务器发生错误，请稍后重试或联系管理员。'
        setFetchFlowError(msg)
      } else {
        // 使用原有的错误处理
        const msg = message || '获取种子信息时发生错误，请查看后台日志。'
        setFetchFlowError(msg)
      }
    } finally {
      isLoading.value = false
    }
  }

  // 检查标准化参数是否符合格式的辅助函数
  const invalidStandardParams = computed(() => {
    const standardizedParams = torrentData.value.standardized_params
    const standardParamKeys: StandardParamKey[] = [
      'type',
      'medium',
      'video_codec',
      'audio_codec',
      'resolution',
      'team',
      'source',
    ]
    const invalidParamsList: Array<StandardParamKey | 'tags'> = []

    // 【修改】使用与 invalidTagsList 相同的、更强大的正则表达式
    const flexibleRegex = new RegExp(/^[\p{L}\p{N}_-]+\.[\p{L}\p{N}_+-]+$/u)

    for (const key of standardParamKeys) {
      const value = standardizedParams[key]

      // 【修改】使用新的正则表达式进行判断
      if (value && typeof value === 'string' && value.trim() !== '' && !flexibleRegex.test(value)) {
        invalidParamsList.push(key)
      }
    }

    // tags：直接在这里做一次校验，避免依赖外部 computed（拆分后更独立）
    const tags = standardizedParams.tags || []
    const invalidTagsCount = tags.filter((tag) => {
      if (!tag || typeof tag !== 'string') return false
      const trimmed = tag.trim()
      if (!trimmed) return false
      return !flexibleRegex.test(trimmed)
    }).length
    if (invalidTagsCount > 0) invalidParamsList.push('tags')

    return invalidParamsList
  })

  const allTagOptions = computed<TagOption[]>(() => {
    const predefinedTags = Object.keys(reverseMappings.value.tags || {})
    const currentTags = torrentData.value.standardized_params.tags || []
    const combined = [...new Set([...predefinedTags, ...currentTags])]

    return combined.map((tagValue) => ({
      value: tagValue,
      label: reverseMappings.value.tags[tagValue] || tagValue,
    }))
  })

  // 辅助函数：处理制作组，去掉横杠
  const cleanTeamValue = (value: string): string => {
    if (!value || typeof value !== 'string') {
      return value
    }
    return value.replace(/^-/, '')
  }

  // 处理制作组输入，自动去掉横杠
  const handleTeamInput = (param: TitleComponent, value: string) => {
    if (param.key === '制作组') {
      param.value = cleanTeamValue(value)
    }
  }

  const goToPublishPreviewStep = async () => {
    // 打印从store获取的已存在站点信息
    console.log('=== 从store获取的已存在站点信息 ===')
    console.log('torrent.value:', torrent.value)
    console.log('torrent.value.sites:', torrent.value?.sites)
    if (torrent.value?.sites) {
      const existingSites = Object.keys(torrent.value.sites)
      console.log('已存在的站点列表:', existingSites)
      console.log('已存在站点详细信息:', torrent.value.sites)
    } else {
      console.log('未找到已存在站点信息')
    }
    console.log('=====================================')

    // 检查是否有不符合格式的标准化参数
    const invalidParams = invalidStandardParams.value
    if (invalidParams.length > 0) {
      // 显示提示信息
      const paramNames: Record<StandardParamKey | 'tags', string> = {
        type: '类型',
        medium: '媒介',
        video_codec: '视频编码',
        audio_codec: '音频编码',
        resolution: '分辨率',
        team: '制作组',
        source: '产地',
        tags: '标签',
      }

      const invalidParamNames = invalidParams.map((param) => paramNames[param] || param)

      ElNotification({
        title: '参数格式不正确',
        message: `以下参数格式不正确，请修改为 *.* 的标准格式: ${invalidParamNames.join(', ')}`,
        type: 'warning',
        duration: 0,
        showClose: true,
      })
      return
    }

    const currentTorrent = torrent.value
    if (!currentTorrent) {
      ElNotification.error({
        title: '参数错误',
        message: '当前种子为空，请刷新后重试',
        duration: 0,
        showClose: true,
      })
      return
    }

    isLoading.value = true
    try {
      ElNotification({
        title: '正在处理',
        message: '正在更新参数并生成预览...',
        type: 'info',
        duration: 0,
      })

      // 从taskId中提取torrent_id和site_name
      // taskId可能格式: db_${torrentId}_${siteName} 或原始task_id
      let torrentId, siteName

      // 如果数据是从数据库加载的，优先使用数据库模式解析
      if (isDataFromDatabase.value && taskId.value && taskId.value.startsWith('db_')) {
        // 数据库模式: db_${torrentId}_${siteName}
        const parts = taskId.value.split('_')
        if (parts.length >= 3) {
          torrentId = parts[1]
          siteName = parts.slice(2).join('_') // 处理站点名称中可能有下划线的情况
        }
      } else if (taskId.value && taskId.value.startsWith('db_')) {
        // 原有的数据库模式解析
        const parts = taskId.value.split('_')
        if (parts.length >= 3) {
          torrentId = parts[1]
          siteName = parts.slice(2).join('_') // 处理站点名称中可能有下划线的情况
        }
      } else {
        // 回退模式：需要从props中获取
        const siteDetails = currentTorrent.sites?.[sourceSite.value]
        if (!siteDetails) {
          throw new Error('无法获取源站点详情（缺少站点映射信息）')
        }
        torrentId = resolveSourceTorrentId({
          sourceInfoTorrentId: sourceTorrentId.value,
          siteDetails,
        })
        siteName = await getEnglishSiteName(sourceSite.value)
      }

      if (!torrentId || !siteName) {
        ElNotification.error({
          title: '参数错误',
          message: '无法获取种子ID或站点名称',
          duration: 0,
          showClose: true,
        })
        return
      }

      console.log(`更新种子参数: ${torrentId} from ${siteName}`)

      // 清理 title_components 中的制作组，去掉横杠
      const cleanedTitleComponents = torrentData.value.title_components.map((component) => {
        if (component.key === '制作组') {
          return {
            ...component,
            value: cleanTeamValue(component.value),
          }
        }
        return component
      })

      // 构建更新的参数，应用空行过滤
      const updatedParameters = {
        title: torrentData.value.original_main_title,
        subtitle: torrentData.value.subtitle,
        imdb_link: torrentData.value.imdb_link,
        douban_link: torrentData.value.douban_link,
        tmdb_link: torrentData.value.tmdb_link,
        screenshot_review_status: torrentData.value.screenshot_review_status,
        poster: torrentData.value.intro.poster,
        screenshots: torrentData.value.intro.screenshots,
        statement: filterExtraEmptyLines(torrentData.value.intro.statement),
        body: filterExtraEmptyLines(torrentData.value.intro.body),
        mediainfo: torrentData.value.mediainfo,
        source_params: torrentData.value.source_params,
        title_components: cleanedTitleComponents,
        // 包含用户修改的标准参数
        standardized_params: torrentData.value.standardized_params,
      }

      console.log('发送到后端的标准参数:', torrentData.value.standardized_params)

      // 调用新的更新接口，此时会将 is_reviewed 设置为 true
      const response = await axios.post('/api/migrate/update_db_seed_info', {
        torrent_name: currentTorrent.name,
        torrent_id: torrentId,
        site_name: siteName,
        updated_parameters: updatedParameters,
      })

      console.log('已调用更新接口，is_reviewed 将被设置为 true')

      ElNotification.closeAll()

      if (response.data.success) {
        ElNotification.closeAll()
        // 更新成功后，获取重新标准化后的参数
        const {
          standardized_params,
          final_publish_parameters,
          complete_publish_params,
          raw_params_for_preview,
          reverse_mappings: updatedReverseMappings,
        } = response.data

        // 更新反向映射表（如果后端返回了更新的映射表）
        if (updatedReverseMappings) {
          reverseMappings.value = updatedReverseMappings
          console.log('成功更新反向映射表:', reverseMappings.value)
        }

        // 更新本地数据，保留用户修改的内容
        torrentData.value = {
          ...torrentData.value,
          standardized_params: standardized_params || {},
          final_publish_parameters: final_publish_parameters || {},
          complete_publish_params: complete_publish_params || {},
          raw_params_for_preview: raw_params_for_preview || {},
        }

        ElNotification.success({
          title: '更新成功',
          message: '参数已更新并重新标准化，请核对预览内容。',
        })

        activeStep.value = 1
      } else {
        ElNotification.error({
          title: '更新失败',
          message: response.data.message || '更新参数失败',
          duration: 0,
          showClose: true,
        })
      }
    } catch (error) {
      ElNotification.closeAll()
      handleApiError(error, '更新预览数据时发生错误，请查看后台日志。')
    } finally {
      isLoading.value = false
    }
  }

  // 【新增】计算属性：整合预设标签和当前已选标签，用于渲染下拉列表
  // 过滤掉禁转标签，防止用户从下拉框选择或取消选择
  const goToSelectSiteStep = async () => {
    const currentTorrent = torrent.value
    if (!currentTorrent) {
      ElNotification.error({
        title: '参数错误',
        message: '当前种子为空，请刷新后重试',
        duration: 0,
        showClose: true,
      })
      return
    }

    // 检查已存在站点数量，如果少于2个则重新获取（因为默认会有源站点本身）
    const existingSitesCount = currentTorrent.sites ? Object.keys(currentTorrent.sites).length : 0

    if (existingSitesCount < 2) {
      console.log(`已存在站点数量不足(${existingSitesCount}个)，正在重新获取种子数据...`)

      try {
        ElNotification.info({
          title: '正在更新数据',
          message: '正在重新获取种子站点信息...',
          duration: 0,
        })

        // 调用后端接口重新获取单个种子数据
        const params = new URLSearchParams({
          page: '1',
          pageSize: '1',
          nameSearch: currentTorrent.name,
        })

        const response = await axios.get(`/api/data?${params.toString()}`)
        const result = response.data

        if (result.error) {
          throw new Error(result.error)
        }

        if (result.data && result.data.length > 0) {
          const updatedTorrent = result.data[0]
          console.log('重新获取到的种子数据:', updatedTorrent)
          console.log('重新获取到的站点信息:', updatedTorrent.sites)
          console.log(
            `站点数量从 ${existingSitesCount} 更新到 ${Object.keys(updatedTorrent.sites).length}`,
          )

          // 更新 store 中的种子信息
          crossSeedStore.setParams(updatedTorrent)

          ElNotification.success({
            title: '数据更新成功',
            message: `已重新获取种子站点信息，发现 ${Object.keys(updatedTorrent.sites).length} 个站点`,
          })
        } else {
          ElNotification.warning({
            title: '未找到种子',
            message: '未能找到匹配的种子数据',
          })
        }
      } catch (error: unknown) {
        console.error('重新获取种子数据时出错:', error)
        const message = axios.isAxiosError(error)
          ? ((error.response?.data as { error?: string; message?: string } | undefined)?.error ||
            (error.response?.data as { message?: string } | undefined)?.message ||
            error.message)
          : error instanceof Error
            ? error.message
            : '重新获取种子数据时发生错误'
        ElNotification.error({
          title: '数据更新失败',
          message,
        })
      }
    } else {
      console.log(`已存在站点数量充足(${existingSitesCount}个)，跳过重新获取`)
    }

    activeStep.value = 2
  }

  const toggleSiteSelection = (siteName: string) => {
    const index = selectedTargetSites.value.indexOf(siteName)
    if (index > -1) {
      selectedTargetSites.value.splice(index, 1)
    } else {
      selectedTargetSites.value.push(siteName)
    }
  }

  const selectAllTargetSites = () => {
    const selectableSites = allSitesStatus.value
      .filter((s) => s.is_target && isTargetSiteSelectable(s.name))
      .map((s) => s.name)
    selectedTargetSites.value = selectableSites
  }

  const clearAllTargetSites = () => {
    selectedTargetSites.value = []
  }

  return {
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
  }
}
