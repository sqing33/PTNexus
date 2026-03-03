import { inject } from 'vue'
import type {
  ComputedRef,
  InjectionKey,
  Ref,
  WritableComputedRef,
} from 'vue'

export interface TitleComponent {
  key: string
  value: string
}

export interface ReverseMappings {
  type: Record<string, string>
  medium: Record<string, string>
  video_codec: Record<string, string>
  audio_codec: Record<string, string>
  resolution: Record<string, string>
  source: Record<string, string>
  team: Record<string, string>
  tags: Record<string, string>
}

export interface StandardizedParams {
  type: string
  medium: string
  video_codec: string
  audio_codec: string
  resolution: string
  team: string
  source: string
  tags: string[]
}

export type StandardParamKey = Exclude<keyof StandardizedParams, 'tags'>

export interface IntroData {
  statement: string
  poster: string
  body: string
  screenshots: string
  removed_ardtudeclarations: string[]
}

export interface TorrentData {
  seed_id: string | null
  title_components: TitleComponent[]
  original_main_title: string
  subtitle: string
  imdb_link: string
  douban_link: string
  tmdb_link: string
  intro: IntroData
  mediainfo: string
  source_params: Record<string, unknown>
  standardized_params: StandardizedParams
  final_publish_parameters: Record<string, string>
  complete_publish_params: Record<string, unknown>
  raw_params_for_preview: Record<string, unknown>
}

export interface BdinfoProgress {
  visible: boolean
  percent: number
  currentFile: string
  elapsedTime: string
  remainingTime: string
}

export interface SiteStatus {
  name: string
  site: string
  has_cookie: boolean
  has_passkey: boolean
  is_source: boolean
  is_target: boolean
  uses_public_publisher?: boolean
  uses_public_extractor?: boolean
}

export type PublishDisplayStatus =
  | 'waiting'
  | 'publishing'
  | 'success'
  | 'warning'
  | 'error'
  | 'paused'

export interface AutoAddResult {
  success: boolean
  message?: string
  downloader_id?: string
  limit_reached?: boolean
  [key: string]: unknown
}

export type PublishDisplayResult = {
  siteName: string
  displayStatus: PublishDisplayStatus
  success?: boolean
  url?: string | null
  logs?: string
  message?: string
  isExisted?: boolean
  // raw fields from backend responses (used by CrossSeedPanel)
  is_existing_torrent?: boolean
  pre_check?: boolean
  limit_reached?: boolean
  auto_add_result?: AutoAddResult | null
  auto_edit_result?: { success?: boolean } | null
  downloaderStatus?: { success: boolean; message?: string; downloaderName?: string } | null
}

export type ElButtonType =
  | 'default'
  | 'primary'
  | 'success'
  | 'warning'
  | 'danger'
  | 'info'
  | 'text'

export interface ProgressCounter {
  current: number
  total: number
}

export interface LimitAlert {
  visible: boolean
  title: string
  message: string
}

export interface TagOption {
  label: string
  value: string
}

export interface CrossSeedPanelContext {
  // Step state
  steps: Array<{ title: string }>
  activeStep: Ref<number>
  activeTab: Ref<string>

  // Core data
  torrentData: Ref<TorrentData>
  reverseMappings: Ref<ReverseMappings>

  // Step 0
  filteredTitleComponents: ComputedRef<TitleComponent[]>
  initialTitleComponents: ComputedRef<TitleComponent[]>
  unrecognizedValue: WritableComputedRef<string>
  invalidStandardParams: ComputedRef<string[]>
  reparseTitle: () => Promise<void>
  isReparsing: Ref<boolean>
  handleTeamInput: (param: TitleComponent, val: string) => void
  allTagOptions: ComputedRef<TagOption[]>
  invalidTagsList: ComputedRef<string[]>
  isRestrictedTag: (tag: string) => boolean
  getTagType: (tag: string) => 'danger' | 'info'
  handleTagClose: (tag: string) => void
  refreshPosters: () => Promise<void>
  isRefreshingPosters: Ref<boolean>
  posterImages: ComputedRef<string[]>
  getProxyImageUrl: (url: string) => string
  handleImageErrorWithProxy: (url: string, type: 'poster' | 'screenshot', index: number) => void
  refreshScreenshots: () => Promise<void>
  isRefreshingScreenshots: Ref<boolean>
  screenshotImages: ComputedRef<string[]>
  refreshIntro: () => Promise<void>
  isRefreshingIntro: Ref<boolean>
  refreshMediainfo: () => Promise<void>
  isRefreshingMediainfo: Ref<boolean>
  bdinfoProgress: Ref<BdinfoProgress>
  discSize: Ref<number>
  formatFileSize: (size: number) => string
  runInBackground: () => void
  stopBDInfoSSE: (showNotification?: boolean | Event) => void
  filteredDeclarationsCount: ComputedRef<number>
  filteredDeclarationsList: ComputedRef<string[]>

  // Step 1
  getMappedValue: (category: StandardParamKey) => string
  getMappedTags: () => string[]
  filteredTags: ComputedRef<string[]>
  parseBBCode: (text?: string | null) => string

  // Step 2
  autoUpdateExistingTorrent: Ref<boolean>
  autoAddExistingToDownloader: Ref<boolean>
  saveAutoAddExistingSetting: () => Promise<void>
  saveAutoUpdateExistingTorrentSetting: () => Promise<void>
  isUbitsDisabled: ComputedRef<boolean>
  allSitesStatus: Ref<SiteStatus[]>
  selectedTargetSites: Ref<string[]>
  selectAllTargetSites: () => void
  clearAllTargetSites: () => void
  getButtonType: (site: SiteStatus) => ElButtonType
  isTargetSiteSelectable: (siteName: string) => boolean
  toggleSiteSelection: (siteName: string) => void
  isAutoUpdateHighlightSite: (site: SiteStatus) => boolean
  isIloliconSite: (site: SiteStatus) => boolean
  isCurrentSeedAnimationRelated: ComputedRef<boolean>

  // Step 3
  publishProgress: Ref<ProgressCounter>
  downloaderProgress: Ref<ProgressCounter>
  limitAlert: Ref<LimitAlert>
  groupedResults: ComputedRef<PublishDisplayResult[][]>
  showSiteLog: (siteName: string, logs: string | undefined) => void
  filterUploadedParam: (url: string) => string
  hasValidUrlsInRow: (row: PublishDisplayResult[]) => boolean
  openAllSitesInRow: (row: PublishDisplayResult[]) => void
  getValidUrlsCount: (row: PublishDisplayResult[]) => number

  // Footer
  showCompleteButton: ComputedRef<boolean>
  isLoading: Ref<boolean>
  isEnqueueing: Ref<boolean>
  isNextButtonDisabled: ComputedRef<boolean>
  nextButtonTooltipContent: ComputedRef<string>
  isScrolledToBottom: Ref<boolean>
  handleCancelClick: () => void
  goToPublishPreviewStep: () => void
  handlePreviousStep: () => void
  handleCompleteClick: () => void
  handleScrollOrNextStep: () => void
  handleEnqueue: () => Promise<void>
  handlePublish: () => Promise<void>
}

export const crossSeedPanelContextKey: InjectionKey<CrossSeedPanelContext> = Symbol(
  'crossSeedPanelContext',
)

export function useCrossSeedPanelContext(): CrossSeedPanelContext {
  const ctx = inject(crossSeedPanelContextKey)
  if (!ctx) {
    throw new Error('useCrossSeedPanelContext must be used within CrossSeedPanel')
  }
  return ctx
}
