import { ElMessage as RawElMessage } from 'element-plus'
import { h, isVNode, type VNodeChild } from 'vue'

type NotifyType = 'success' | 'warning' | 'info' | 'error' | 'primary'
type NotifyInput = string | number | NotifyOptions
type MaybeFactory<T> = T | (() => T)

interface NotifyOptions {
  message?: MaybeFactory<unknown>
  type?: NotifyType
  duration?: number
  showClose?: boolean
  center?: boolean
  grouping?: boolean
  customClass?: string
  offset?: number
  onClose?: () => void
  title?: string
  [key: string]: unknown
}

interface MessageHandler {
  close: () => void
}

interface NotifyApi {
  (options?: NotifyInput): MessageHandler
  success: (options?: NotifyInput) => MessageHandler
  warning: (options?: NotifyInput) => MessageHandler
  info: (options?: NotifyInput) => MessageHandler
  error: (options?: NotifyInput) => MessageHandler
  closeAll: () => void
}

const MESSAGE_OFFSET = 72
const DEDUPE_WINDOW_MS = 1400
const dedupeCache = new Map<string, number>()
const notificationHandlers = new Set<MessageHandler>()
const noopHandler: MessageHandler = { close: () => undefined }

const rawElMessage = RawElMessage as unknown as NotifyApi

const getPlainText = (value: unknown): string => {
  if (typeof value === 'string' || typeof value === 'number') {
    return String(value).trim()
  }
  return ''
}

const resolveContent = (value: MaybeFactory<unknown>): unknown =>
  typeof value === 'function' ? (value as () => unknown)() : value

const normalizeInput = (input?: NotifyInput): NotifyOptions => {
  if (typeof input === 'string' || typeof input === 'number') {
    return { message: String(input) }
  }
  return input ? { ...input } : {}
}

const joinClass = (left: string | undefined, right: string): string =>
  [left, right].filter(Boolean).join(' ').trim()

const shouldSkip = (key: string): boolean => {
  if (!key) {
    return false
  }
  const now = Date.now()
  const previous = dedupeCache.get(key)
  dedupeCache.set(key, now)
  return typeof previous === 'number' && now - previous < DEDUPE_WINDOW_MS
}

const buildDedupKey = (type: NotifyType | undefined, title: string, message: unknown): string => {
  const plainBody = getPlainText(message)
  const plainTitle = title.trim()
  if (!plainBody && !plainTitle) {
    return ''
  }
  return `${type || 'plain'}|${plainTitle}|${plainBody}`
}

const buildMessageOptions = (input?: NotifyInput, fallbackType?: NotifyType): NotifyOptions => {
  const options = normalizeInput(input)

  if (fallbackType && !options.type) {
    options.type = fallbackType
  }
  options.message = resolveContent(options.message ?? '')
  options.center = true
  options.grouping = true
  options.showClose = options.showClose ?? true
  options.offset = typeof options.offset === 'number' ? options.offset : MESSAGE_OFFSET
  options.customClass = joinClass(options.customClass, 'ptn-status-toast')
  if (options.type) {
    options.customClass = joinClass(options.customClass, `ptn-status-toast--${options.type}`)
  }

  return options
}

const showMessage = (input?: NotifyInput, fallbackType?: NotifyType): MessageHandler => {
  const options = buildMessageOptions(input, fallbackType)
  if (shouldSkip(buildDedupKey(options.type, '', options.message))) {
    return noopHandler
  }
  return rawElMessage(options)
}

const buildNotificationContent = (title: string, message: unknown): VNodeChild => {
  const content = resolveContent(message)
  const trimmedTitle = title.trim()

  if (!trimmedTitle) {
    if (isVNode(content)) {
      return content
    }
    if (typeof content === 'string' || typeof content === 'number') {
      return String(content)
    }
    return getPlainText(content)
  }

  const bodyContent =
    isVNode(content) || typeof content === 'string' || typeof content === 'number'
      ? content
      : getPlainText(content)

  return h('div', { class: 'ptn-status-toast__content' }, [
    h('div', { class: 'ptn-status-toast__title' }, trimmedTitle),
    h('div', { class: 'ptn-status-toast__body' }, bodyContent || ''),
  ])
}

const showNotification = (input?: NotifyInput, fallbackType?: NotifyType): MessageHandler => {
  const options = normalizeInput(input)
  const title = getPlainText(options.title)
  const body = resolveContent(options.message ?? '')
  const { title: _discardTitle, ...rest } = options

  const merged = buildMessageOptions(
    {
      ...rest,
      type: options.type || fallbackType || 'info',
      duration: options.duration ?? 4500,
      message: buildNotificationContent(title, body),
      customClass: joinClass(options.customClass, 'ptn-status-toast--notification'),
    },
    undefined,
  )

  if (shouldSkip(buildDedupKey(merged.type, title, body))) {
    return noopHandler
  }

  const originalOnClose = merged.onClose
  let handler: MessageHandler = noopHandler
  merged.onClose = () => {
    notificationHandlers.delete(handler)
    originalOnClose?.()
  }
  handler = rawElMessage(merged)
  notificationHandlers.add(handler)

  return handler
}

export const ElMessage = ((input?: NotifyInput) => showMessage(input)) as NotifyApi
ElMessage.success = (input?: NotifyInput) => showMessage(input, 'success')
ElMessage.warning = (input?: NotifyInput) => showMessage(input, 'warning')
ElMessage.info = (input?: NotifyInput) => showMessage(input, 'info')
ElMessage.error = (input?: NotifyInput) => showMessage(input, 'error')
ElMessage.closeAll = () => {
  rawElMessage.closeAll()
}

export const ElNotification = ((input?: NotifyInput) =>
  showNotification(input, 'info')) as NotifyApi
ElNotification.success = (input?: NotifyInput) => showNotification(input, 'success')
ElNotification.warning = (input?: NotifyInput) => showNotification(input, 'warning')
ElNotification.info = (input?: NotifyInput) => showNotification(input, 'info')
ElNotification.error = (input?: NotifyInput) => showNotification(input, 'error')
ElNotification.closeAll = () => {
  notificationHandlers.forEach((handler) => handler.close())
  notificationHandlers.clear()
}
